import {
  Component as ReactComponent,
  type ReactNode,
  useEffect,
  useLayoutEffect,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { api, APIError, advanceSession, onSessionExpired } from "./client";
import { client, clearDrafts, Session, useResource, useSession } from "./data";
import type { Me, Workspace } from "./contracts";
import {
  Button,
  ErrorView,
  Field,
  Form,
  Icon,
  IdentityIcon,
  Loading,
  Logo,
} from "./ui";
import { navigate, useRoute } from "./navigation";
import { recordNavigationPaint } from "./performance";
import { PageLoader, usePage } from "./route-loader";
import { useLive } from "./live";
import { AccountView, type Account } from "./AccountView";

const modules = {
  agents: () => import("./pages/Directory"),
  people: () => import("./pages/Directory"),
  groups: () => import("./pages/Groups"),
  inbox: () => import("./pages/Inbox"),
  requests: () => import("./pages/Inbox"),
  conversations: () => import("./pages/Conversations"),
  permissions: () => import("./pages/Permissions"),
  integrations: () => import("./pages/Integrations"),
  activity: () => import("./pages/Activity"),
  settings: () => import("./pages/Settings"),
};
const pages = new PageLoader(modules);
const links = [
  ["inbox", "Inbox"],
  ["conversations", "Conversations"],
  ["agents", "Agents"],
  ["people", "People"],
  ["groups", "Groups"],
  ["permissions", "Permissions"],
  ["integrations", "Integrations"],
  ["activity", "Activity"],
];

export default function App() {
  const [epoch, setEpoch] = useState(0);
  const [logoutError, setLogoutError] = useState<Error | null>(null);
  const me = useQuery({
    queryKey: ["session", epoch],
    queryFn: ({ signal }) => api<Me>("/me", undefined, { signal }),
    retry: false,
    refetchInterval: 15_000,
  });
  const account = useQuery({
    queryKey: ["session-account", epoch],
    queryFn: ({ signal }) =>
      api<Account>("/workspace/account", undefined, { signal }),
    enabled: !!me.error,
    retry: false,
  });
  useEffect(
    () =>
      onSessionExpired(() => {
        advanceSession();
        void client.cancelQueries();
        client.clear();
        clearDrafts();
        setEpoch((x) => x + 1);
      }),
    [],
  );
  useEffect(() => {
    if (me.error instanceof APIError && me.error.status === 401) {
      clearDrafts();
      void client.cancelQueries({
        predicate: (q) => String(q.queryKey[0]).includes("/"),
      });
      client.removeQueries({
        predicate: (q) => String(q.queryKey[0]).includes("/"),
      });
    }
  }, [me.error]);
  if (me.isPending)
    return (
      <div className="startup">
        <Logo />
        <Loading label="Opening workspace" />
      </div>
    );
  if (me.error) {
    if (account.isPending)
      return (
        <div className="startup">
          <Logo />
          <Loading label="Checking workspace access" />
        </div>
      );
    if (account.data && !account.error)
      return (
        <main className="access-unavailable">
          <Logo />
          <ErrorView error={me.error} />
          <AccountView account={account.data} />
          <ErrorView error={logoutError} />
          <div className="actions">
            <Button primary onClick={() => void me.refetch()}>
              Check workspace access again
            </Button>
            <Button
              onClick={() =>
                void api("/auth/logout", {})
                  .then(() => {
                    advanceSession();
                    client.clear();
                    clearDrafts();
                    setEpoch((x) => x + 1);
                  })
                  .catch(setLogoutError)
              }
            >
              Sign out
            </Button>
          </div>
        </main>
      );
    if (me.error instanceof APIError && me.error.status === 401)
      return (
        <Login
          onDone={() => {
            advanceSession();
            clearDrafts();
            client.clear();
            setEpoch((x) => x + 1);
          }}
        />
      );
    return (
      <div className="startup">
        <Logo />
        <ErrorView error={me.error} retry={() => void me.refetch()} />
      </div>
    );
  }
  if (!me.data?.person)
    return (
      <div className="startup">
        <ErrorView
          error={
            new Error("A Person account is required to open the workspace.")
          }
        />
      </div>
    );
  return (
    <Session.Provider value={me.data}>
      <WorkspaceApp
        key={`${me.data.company}/${me.data.environment}/${me.data.person.id}/${epoch}`}
        onLogout={() => {
          advanceSession();
          client.clear();
          clearDrafts();
          setEpoch((x) => x + 1);
        }}
      />
    </Session.Provider>
  );
}
function Login({ onDone }: { onDone: () => void }) {
  const methods = useQuery({
    queryKey: ["auth-methods"],
    queryFn: ({ signal }) =>
      api<{ token: boolean; oidc: boolean }>("/auth/methods", undefined, {
        signal,
      }),
  });
  const [token, setToken] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<Error | null>(null);
  return (
    <main className="login">
      <div className="login-panel">
        <Logo />
        <h1>Your workspace, connected.</h1>
        <p>Sign in to your Beep Grid workspace.</p>
        <ErrorView error={methods.error || error} />
        {methods.isPending ? <Loading /> : null}
        {methods.data?.oidc ? (
          <a className="button primary" href="/auth/oidc/start">
            Continue with company sign-in
            <Icon name="arrow" size={16} />
          </a>
        ) : null}
        {methods.data?.token ? (
          <Form
            onSubmit={() => {
              setBusy(true);
              setError(null);
              void api("/auth/login", { token })
                .then(() => {
                  setToken("");
                  onDone();
                })
                .catch(setError)
                .finally(() => setBusy(false));
            }}
          >
            <Field
              label="Person access token"
              hint="Your administrator can provide a Person credential."
            >
              <input
                type="password"
                autoComplete="off"
                required
                value={token}
                onChange={(e) => setToken(e.target.value)}
              />
            </Field>
            <Button primary disabled={busy || !token.trim()} type="submit">
              {busy ? "Signing in" : "Open workspace"}
              <Icon name="arrow" size={16} />
            </Button>
          </Form>
        ) : null}
        <small>Agent and service credentials cannot sign in here.</small>
      </div>
      <footer>beepgrid.com</footer>
    </main>
  );
}
function WorkspaceApp({ onLogout }: { onLogout: () => void }) {
  const me = useSession(),
    route = useRoute(),
    meta = useResource<Workspace>("/workspace");
  const live = useLive(route.page === "conversations" ? route.id : "");
  const [menu, setMenu] = useState(false),
    [error, setError] = useState<Error | null>(null),
    [signingOut, setSigningOut] = useState(false);
  useEffect(() => {
    if (!menu) return;
    const previous = document.activeElement as HTMLElement | null;
    (document.querySelector(".sidebar nav a") as HTMLElement | null)?.focus();
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setMenu(false);
      }
    };
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("keydown", escape);
      previous?.focus();
    };
  }, [menu]);
  const [theme, setTheme] = useState(() => {
    try {
      const saved = localStorage.getItem("beepgrid:workspace:theme");
      return saved && ["light", "dark", "system"].includes(saved)
        ? saved
        : "system";
    } catch {
      return "system";
    }
  });
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      document.documentElement.dataset.theme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme;
    };
    apply();
    try {
      localStorage.setItem("beepgrid:workspace:theme", theme);
    } catch {}
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  useEffect(() => {
    setMenu(false);
    document.title =
      (links.find(
        (x) => x[0] === (route.page === "requests" ? "inbox" : route.page),
      )?.[1] || "Settings") + " · beepgrid";
  }, [route.page]);
  const page =
    route.page === "requests"
      ? "inbox"
      : route.page === "connections"
        ? "integrations"
        : route.page;
  const section = usePage(pages, page);
  const Component = section.component;
  function link(path: string) {
    setMenu(false);
    navigate("/" + path);
  }
  return (
    <div className="workspace">
      <a href="#main" className="skip-link">
        Skip to workspace
      </a>
      {menu ? (
        <div className="nav-backdrop" onClick={() => setMenu(false)} />
      ) : null}
      <aside
        className={`sidebar ${menu ? "expanded" : ""}`}
        aria-label="Workspace navigation"
      >
        <a
          className="brand-link"
          href="/agents"
          onClick={(e) => {
            e.preventDefault();
            link("agents");
          }}
        >
          <Logo />
        </a>
        <div className="workspace-context">
          <span className="workspace-avatar">{me.company.slice(0, 1)}</span>
          <div>
            <strong>{me.company}</strong>
            <span>{me.environment}</span>
          </div>
          <BadgeDeployment deployment={meta.data?.deployment} />
        </div>
        <nav>
          {links.map(([id, title]) => (
            <a
              key={id}
              href={"/" + id}
              aria-current={page === id ? "page" : undefined}
              onMouseEnter={() => void pages.load(id).catch(() => {})}
              onFocus={() => void pages.load(id).catch(() => {})}
              onClick={(e) => {
                if (e.metaKey || e.ctrlKey) return;
                e.preventDefault();
                link(id);
              }}
            >
              <Icon name={id} />
              <span>{title}</span>
              {page === id ? <i /> : null}
            </a>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <a
            href="/settings"
            aria-current={page === "settings" ? "page" : undefined}
            onClick={(e) => {
              e.preventDefault();
              link("settings");
            }}
          >
            <Icon name="settings" />
            <span>Settings</span>
          </a>
          <div className="theme-switch" aria-label="Color theme">
            {[
              ["light", "sun"],
              ["dark", "moon"],
              ["system", "monitor"],
            ].map(([id, icon]) => (
              <button
                key={id}
                aria-label={`${id} theme`}
                aria-pressed={theme === id}
                onClick={() => setTheme(id)}
              >
                <Icon name={icon} size={15} />
              </button>
            ))}
          </div>
          <div className="profile">
            <IdentityIcon
              id={me.person.id}
              kind="person"
              label={me.person.name}
            />
            <div>
              <strong>{me.person.name}</strong>
              <span>
                {me.admin ? "Workspace administrator" : "Workspace member"}
              </span>
            </div>
            <button
              className="icon-button"
              title="Sign out"
              aria-label="Sign out"
              disabled={signingOut}
              onClick={() => {
                setSigningOut(true);
                void api("/auth/logout", {})
                  .then(onLogout)
                  .catch(setError)
                  .finally(() => setSigningOut(false));
              }}
            >
              <Icon name="logout" size={16} />
            </button>
          </div>
        </div>
      </aside>
      <div className="main-shell" inert={menu || undefined}>
        <header className="topbar">
          <button
            className="icon-button mobile-menu"
            aria-label="Open navigation"
            onClick={() => setMenu(true)}
          >
            <Icon name="menu" />
          </button>
          <span>{me.company}</span>
          <span className="slash">/</span>
          <span>{links.find((x) => x[0] === page)?.[1] || "Settings"}</span>
          <span className="live-indicator">
            <i className={live === "Live" ? "good" : ""} />
            {live}
          </span>
          <span className="environment">{me.environment}</span>
        </header>
        <main id="main" data-page={page} tabIndex={-1}>
          <ErrorView error={error} />
          <SectionBoundary key={page}>
            {section.error ? (
              <ErrorView error={section.error} retry={section.retry} />
            ) : Component ? (
              <>
                <Component key={page} section={page} />
                <PaintReady route={route.page + route.id} />
              </>
            ) : (
              <Loading label="Opening section" />
            )}
          </SectionBoundary>
        </main>
      </div>
    </div>
  );
}
function BadgeDeployment({ deployment }: { deployment?: string }) {
  return deployment ? (
    <span className="deployment" title={deployment.replaceAll("_", " ")}>
      <Icon
        name={deployment === "self_hosted" ? "service" : "globe"}
        size={13}
      />
    </span>
  ) : null;
}

class SectionBoundary extends ReactComponent<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? (
      <ErrorView
        error={
          new Error(
            "This section could not open. Reload to get the latest workspace app.",
          )
        }
        retry={() => window.location.reload()}
      />
    ) : (
      this.props.children
    );
  }
}

function PaintReady({ route }: { route: string }) {
  useLayoutEffect(recordNavigationPaint, [route]);
  return null;
}
