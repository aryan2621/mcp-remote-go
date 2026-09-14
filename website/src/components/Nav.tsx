import { useState } from "react";
import { Link, NavLink } from "react-router-dom";
import logo from "../assets/logo.svg";
import { LINKS } from "../links";
import { ThemeToggle } from "./theme-toggle";

const navItems = [
  { to: "/install", label: "Install" },
  { to: "/download", label: "Binaries" },
  { to: "/faq", label: "FAQ" },
];

export function Nav() {
  const [open, setOpen] = useState(false);

  return (
    <header className="border-b border-border bg-background">
      <div className="mx-auto flex h-14 max-w-5xl items-center gap-6 px-5">
        <Link to="/" className="flex items-center gap-2 font-mono text-sm" onClick={() => setOpen(false)}>
          <img src={logo} alt="" className="h-6 w-6" width={24} height={24} />
          mcp-remote-go
        </Link>
        <nav className="hidden items-center gap-5 text-sm text-muted-foreground md:flex">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) => (isActive ? "text-foreground" : "hover:text-foreground")}
            >
              {item.label}
            </NavLink>
          ))}
          <a href={LINKS.github} target="_blank" rel="noopener noreferrer" className="hover:text-foreground">
            GitHub
          </a>
        </nav>
        <div className="ml-auto flex items-center gap-1">
          <ThemeToggle />
          <Link to="/download" className="btn btn-primary hidden md:inline-flex">
            Get binary
          </Link>
          <button
            type="button"
            className="btn btn-ghost md:hidden"
            aria-expanded={open}
            aria-label="Menu"
            onClick={() => setOpen((value) => !value)}
          >
            {open ? "Close" : "Menu"}
          </button>
        </div>
      </div>
      {open ? (
        <div className="flex flex-col gap-3 border-t border-border px-5 py-4 text-sm md:hidden">
          {navItems.map((item) => (
            <NavLink key={item.to} to={item.to} onClick={() => setOpen(false)}>
              {item.label}
            </NavLink>
          ))}
          <a href={LINKS.github} target="_blank" rel="noopener noreferrer">
            GitHub
          </a>
        </div>
      ) : null}
    </header>
  );
}
