package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

type Proxy struct {
	remoteTransport RemoteTransport
	debugLog        *log.Logger
}

func NewProxy(remote RemoteTransport, debugLog *log.Logger) *Proxy {
	return &Proxy{
		remoteTransport: remote,
		debugLog:        debugLog,
	}
}

func (p *Proxy) Run(ctx context.Context) error {
	p.LogDebug("Starting proxy")

	if err := p.remoteTransport.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to remote: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer p.Cleanup(ctx)

	p.LogDebug("Connected to remote server")

	stdinChan := make(chan []byte, 10)
	remoteChan := make(chan []byte, 10)
	errChan := make(chan error, 2)

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			msg := make([]byte, len(line))
			copy(msg, line)

			select {
			case stdinChan <- msg:
				p.LogDebug("Read from stdin: %s", string(msg))
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil && err != io.EOF {
			errChan <- fmt.Errorf("stdin error: %w", err)
		}
		p.LogDebug("Stdin reader closed")
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				data, err := p.remoteTransport.Receive(ctx)
				if err != nil {
					if err != io.EOF && ctx.Err() == nil {
						p.LogDebug("Remote receive error: %v", err)
					}
					return
				}

				select {
				case remoteChan <- data:
					p.LogDebug("Received from remote: %s", string(data))
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	p.LogDebug("Entering proxy loop")
	for {
		select {
		case <-ctx.Done():
			p.LogDebug("Context cancelled")
			return ctx.Err()

		case err := <-errChan:
			return err

		case msg := <-stdinChan:
			p.LogDebug("Forwarding stdin -> remote")
			if err := p.SendWithRetry(ctx, msg); err != nil {
				return fmt.Errorf("failed to send to remote: %w", err)
			}

		case msg := <-remoteChan:
			p.LogDebug("Forwarding remote -> stdout")
			if _, err := os.Stdout.Write(msg); err != nil {
				return fmt.Errorf("failed to write to stdout: %w", err)
			}
			if _, err := os.Stdout.Write([]byte("\n")); err != nil {
				return fmt.Errorf("failed to write newline: %w", err)
			}
		}
	}
}

func (p *Proxy) SendWithRetry(ctx context.Context, msg []byte) error {
	maxRetries := 3
	retryDelay := 1 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := p.remoteTransport.Send(ctx, msg)
		if err == nil {
			return nil
		}

		if transportErr, ok := err.(*TransportError); ok {
			if !transportErr.IsRetryable {
				return err
			}

			if transportErr.StatusCode == 404 {
				p.LogDebug("Session expired, reconnecting...")
				if reconnectErr := p.remoteTransport.Connect(ctx); reconnectErr != nil {
					return fmt.Errorf("failed to reconnect: %w", reconnectErr)
				}
				continue
			}

			if transportErr.StatusCode == 401 {
				return fmt.Errorf("authentication failed, please restart: %w", err)
			}
		}

		if attempt < maxRetries {
			p.LogDebug("Retry attempt %d/%d after error: %v", attempt+1, maxRetries, err)
			select {
			case <-time.After(retryDelay):
				retryDelay *= 2
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			return err
		}
	}

	return fmt.Errorf("max retries exceeded")
}

func (p *Proxy) Cleanup(ctx context.Context) {
	p.LogDebug("Cleaning up proxy")

	if sa, ok := p.remoteTransport.(SessionAwareTransport); ok {
		if err := sa.TerminateSession(ctx); err != nil {
			p.LogDebug("Failed to terminate session: %v", err)
		}
	}

	if err := p.remoteTransport.Close(); err != nil {
		p.LogDebug("Failed to close transport: %v", err)
	}
}

func (p *Proxy) LogDebug(format string, args ...interface{}) {
	if p.debugLog != nil {
		p.debugLog.Printf(format, args...)
	}
}