import { useCallback, useEffect, useState, type ReactNode } from "react";
import { chatStream, getAPIBaseLabel, initSession, type StreamEvent } from "./api/client";

type WinId = "terminal" | "browser" | "trash";

function formatAgentEvent(ev: StreamEvent): string {
  if (ev.type === "agent.message" && ev.content != null) {
    const c = ev.content;
    if (typeof c === "string") return c;
    if (Array.isArray(c)) {
      return c
        .map((block: { type?: string; text?: string }) =>
          block?.type === "text" && block.text != null ? block.text : JSON.stringify(block)
        )
        .join("");
    }
  }
  if (ev.type === "ocean.done" || ev.type === "session.status_idle") return "";
  return JSON.stringify(ev);
}

export default function App() {
  const apiBase = getAPIBaseLabel();
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);

  const [open, setOpen] = useState<Record<WinId, boolean>>({
    terminal: true,
    browser: false,
    trash: false,
  });
  const [focus, setFocus] = useState<WinId>("terminal");
  const [zMap, setZMap] = useState<Record<WinId, number>>({
    terminal: 10,
    browser: 9,
    trash: 8,
  });

  const bring = (id: WinId) => {
    setFocus(id);
    setZMap((m) => {
      const top = Math.max(m.terminal, m.browser, m.trash, 0) + 1;
      return { ...m, [id]: top };
    });
  };

  const [termLog, setTermLog] = useState<string[]>([
    "Ocean OS — cloud agent connected to this desktop.",
    "Type a command or question and press Enter.",
  ]);
  const [browserLog, setBrowserLog] = useState<string[]>([
    "Enter a URL or search terms. The agent sees what you send from this window.",
  ]);
  const [trashLog, setTrashLog] = useState<string[]>([
    "Trash contains sample items. Ask the agent to “analyze trash” or recover files.",
  ]);

  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const { sessionId: sid } = await initSession();
        if (!cancelled) {
          setSessionId(sid);
          setReady(true);
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const runAgent = useCallback(
    async (component: WinId, text: string, append: (line: string) => void) => {
      if (!text.trim() || busy) return;
      setBusy(true);
      append(`\n> ${text}\n`);
      try {
        await chatStream(component, text, (ev) => {
          if (ev.type === "agent.message") {
            const line = formatAgentEvent(ev);
            if (line) append(line + "\n");
          }
        });
      } catch (e) {
        append(`Error: ${e instanceof Error ? e.message : String(e)}\n`);
      } finally {
        setBusy(false);
      }
    },
    [busy]
  );

  const clock = useClock();

  return (
    <div className="desktop">
      <div className="wallpaper" aria-hidden />
      <header className="topbar">
        <span className="topbar__activities">Activities</span>
        <span className="topbar__title">Ocean OS</span>
        <span className="topbar__clock">{clock}</span>
      </header>

      {!ready && !error && <div className="boot">Booting session…</div>}
      {error && (
        <div className="boot boot--err">
          <p>Could not reach API. Start the Go server:</p>
          <code>go run ./cmd/server</code>
          <p className="muted">API target: {apiBase}</p>
          <p className="muted">{error}</p>
        </div>
      )}

      {ready && (
        <main className="workspace">
          {open.terminal && (
            <Window
              title="Terminal"
              icon=">_"
              z={zMap.terminal}
              focused={focus === "terminal"}
              onFocus={() => bring("terminal")}
              onClose={() => setOpen((o) => ({ ...o, terminal: false }))}
            >
              <TerminalBody
                lines={termLog}
                disabled={busy}
                onSubmit={(line) =>
                  runAgent("terminal", line, (l) => setTermLog((t) => [...t, l]))
                }
              />
            </Window>
          )}

          {open.browser && (
            <Window
              title="Web Browser"
              icon="◉"
              z={zMap.browser}
              focused={focus === "browser"}
              onFocus={() => bring("browser")}
              onClose={() => setOpen((o) => ({ ...o, browser: false }))}
            >
              <BrowserBody
                lines={browserLog}
                disabled={busy}
                onGo={(url, note) =>
                  runAgent(
                    "browser",
                    JSON.stringify({ url, note }),
                    (l) => setBrowserLog((b) => [...b, l])
                  )
                }
              />
            </Window>
          )}

          {open.trash && (
            <Window
              title="Trash"
              icon="🗑"
              z={zMap.trash}
              focused={focus === "trash"}
              onFocus={() => bring("trash")}
              onClose={() => setOpen((o) => ({ ...o, trash: false }))}
            >
              <TrashBody
                lines={trashLog}
                disabled={busy}
                onAsk={(q) =>
                  runAgent("trash", q, (l) => setTrashLog((t) => [...t, l]))
                }
              />
            </Window>
          )}
        </main>
      )}

      <nav className="dock" aria-label="Applications">
        <DockBtn
          label="Terminal"
          active={open.terminal && focus === "terminal"}
          onClick={() => {
            setOpen((o) => ({ ...o, terminal: true }));
            bring("terminal");
          }}
        >
          <span className="dock__ico dock__ico--term">{">_"}</span>
        </DockBtn>
        <DockBtn
          label="Browser"
          active={open.browser && focus === "browser"}
          onClick={() => {
            setOpen((o) => ({ ...o, browser: true }));
            bring("browser");
          }}
        >
          <span className="dock__ico">◉</span>
        </DockBtn>
        <DockBtn
          label="Trash"
          active={open.trash && focus === "trash"}
          onClick={() => {
            setOpen((o) => ({ ...o, trash: true }));
            bring("trash");
          }}
        >
          <span className="dock__ico">🗑</span>
        </DockBtn>
      </nav>

      {ready && sessionId && (
        <footer className="statusbar">
          <span>session {sessionId.slice(0, 12)}…</span>
          <span>api {apiBase}</span>
          {busy && <span className="pulse">agent working…</span>}
        </footer>
      )}
    </div>
  );
}

function useClock() {
  const [t, setT] = useState(() => new Date().toLocaleTimeString());
  useEffect(() => {
    const id = setInterval(() => setT(new Date().toLocaleTimeString()), 1000);
    return () => clearInterval(id);
  }, []);
  return t;
}

function Window(props: {
  title: string;
  icon: string;
  z: number;
  focused: boolean;
  onFocus: () => void;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <section
      className={"window " + (props.focused ? "window--focus" : "")}
      style={{ zIndex: props.z }}
      onMouseDown={props.onFocus}
    >
      <div className="window__titlebar">
        <span className="window__dots" aria-hidden>
          <i className="dot dot--r" />
          <i className="dot dot--y" />
          <i className="dot dot--g" />
        </span>
        <span className="window__title">
          <span className="window__icon">{props.icon}</span> {props.title}
        </span>
        <button type="button" className="window__close" onClick={props.onClose} aria-label="Close">
          ×
        </button>
      </div>
      <div className="window__body">{props.children}</div>
    </section>
  );
}

function TerminalBody(props: {
  lines: string[];
  disabled: boolean;
  onSubmit: (line: string) => void;
}) {
  const [input, setInput] = useState("");
  return (
    <div className="term">
      <pre className="term__out">{props.lines.join("")}</pre>
      <form
        className="term__form"
        onSubmit={(e) => {
          e.preventDefault();
          props.onSubmit(input);
          setInput("");
        }}
      >
        <span className="term__prompt">user@ocean:~$</span>
        <input
          className="term__in"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          disabled={props.disabled}
          autoComplete="off"
          spellCheck={false}
        />
      </form>
    </div>
  );
}

function BrowserBody(props: {
  lines: string[];
  disabled: boolean;
  onGo: (url: string, note: string) => void;
}) {
  const [url, setUrl] = useState("https://example.com");
  const [note, setNote] = useState("");
  return (
    <div className="browser">
      <div className="browser__toolbar">
        <input
          className="browser__url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          disabled={props.disabled}
          placeholder="https://…"
        />
        <button type="button" disabled={props.disabled} onClick={() => props.onGo(url, note)}>
          Go / Ask agent
        </button>
      </div>
      <input
        className="browser__note"
        value={note}
        onChange={(e) => setNote(e.target.value)}
        disabled={props.disabled}
        placeholder="Optional note to send with the URL…"
      />
      <iframe className="browser__frame" title="preview" src={url} sandbox="allow-scripts allow-same-origin" />
      <pre className="browser__agent">{props.lines.join("")}</pre>
    </div>
  );
}

const trashItems = ["readme.txt", "draft.png", "old_logs/"];

function TrashBody(props: {
  lines: string[];
  disabled: boolean;
  onAsk: (q: string) => void;
}) {
  const [q, setQ] = useState("");
  return (
    <div className="trash">
      <ul className="trash__list">
        {trashItems.map((name) => (
          <li key={name}>
            <span className="trash__ico">📄</span> {name}
          </li>
        ))}
      </ul>
      <form
        className="trash__form"
        onSubmit={(e) => {
          e.preventDefault();
          props.onAsk(q);
          setQ("");
        }}
      >
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          disabled={props.disabled}
          placeholder="Ask the agent about these files…"
        />
        <button type="submit" disabled={props.disabled}>
          Send
        </button>
      </form>
      <pre className="trash__out">{props.lines.join("")}</pre>
    </div>
  );
}

function DockBtn(props: {
  label: string;
  active?: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button type="button" className={"dock__btn " + (props.active ? "dock__btn--active" : "")} title={props.label} onClick={props.onClick}>
      {props.children}
    </button>
  );
}
