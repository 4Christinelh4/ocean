export type StreamEvent = Record<string, unknown> & { type?: string };

const API_BASE = (import.meta.env.VITE_API_BASE ?? "").trim().replace(/\/$/, "");

function apiURL(path: string): string {
  if (!API_BASE) return path;
  return `${API_BASE}${path}`;
}

export function getAPIBaseLabel(): string {
  return API_BASE || "same-origin (/api via Vite proxy in dev)";
}

export async function initSession(): Promise<{ sessionId: string }> {
  const res = await fetch(apiURL("/api/init"), { method: "POST" });
  if (!res.ok) throw new Error(await res.text());
  return res.json() as Promise<{ sessionId: string }>;
}

/** POST /api/chat — response is SSE: each `data: ` line is JSON (Anthropic event or `{type:ocean.done}`). */
export async function chatStream(
  type: string,
  command: string,
  input: string,
  onEvent: (ev: StreamEvent) => void
): Promise<void> {
  const res = await fetch(apiURL("/api/chat"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ type, command, input }),
  });
  if (!res.ok || !res.body) {
    throw new Error(await res.text());
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let sep: number;
    while ((sep = buffer.indexOf("\n\n")) >= 0) {
      const block = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      for (const line of block.split("\n")) {
        if (line.startsWith("data:")) {
          const raw = line.slice(5).trim();
          if (!raw || raw === "[DONE]") continue;
          try {
            const ev = JSON.parse(raw) as StreamEvent;
            onEvent(ev);
            if (ev.type === "ocean.done") return;
          } catch {
            /* ignore partial */
          }
        }
      }
    }
  }
}
