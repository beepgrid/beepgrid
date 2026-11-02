import { useEffect, useState, type ComponentType } from "react";
export type PageComponent = ComponentType<{ section: string }>;
export class PageLoader {
  private cached = new Map<string, PageComponent>();
  private pending = new Map<string, Promise<PageComponent>>();
  constructor(
    private loaders: Record<string, () => Promise<{ default: PageComponent }>>,
  ) {}
  peek(name: string) {
    return this.cached.get(name);
  }
  load(name: string): Promise<PageComponent> {
    const cached = this.peek(name);
    if (cached) return Promise.resolve(cached);
    const pending = this.pending.get(name);
    if (pending) return pending;
    const load = this.loaders[name];
    if (!load)
      return Promise.reject(Error("This workspace section does not exist."));
    const task = load()
      .then((module) => {
        this.cached.set(name, module.default);
        return module.default;
      })
      .finally(() => this.pending.delete(name));
    this.pending.set(name, task);
    return task;
  }
}
export function usePage(loader: PageLoader, name: string) {
  const [state, setState] = useState<{
    name: string;
    component?: PageComponent;
    error?: Error;
  }>({ name, component: loader.peek(name) });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let current = true;
    setState({ name, component: loader.peek(name) });
    void loader
      .load(name)
      .then((component) => {
        if (current) setState({ name, component });
      })
      .catch((error) => {
        if (current)
          setState({
            name,
            error:
              error instanceof Error
                ? error
                : Error("This section could not open."),
          });
      });
    return () => {
      current = false;
    };
  }, [loader, name, attempt]);
  return {
    component:
      loader.peek(name) || (state.name === name ? state.component : undefined),
    error: state.name === name ? state.error : undefined,
    retry: () => setAttempt((n) => n + 1),
  };
}
