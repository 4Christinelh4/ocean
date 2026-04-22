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

/**
 * Streams chat events from the server endpoint `/api/chat` and calls `onEvent` for each parsed event.
 *
 * Sends a JSON POST containing `type`, `command`, and `input`. Processes the response as an SSE-style stream where each `data:` line is parsed as JSON and delivered to `onEvent`. Streaming stops early if an event with `type === "ocean.done"` is received.
 *
 * @param type - Message category or processing mode sent to the API
 * @param command - Command or instruction included with the request payload
 * @param input - User-provided input text for the chat request
 * @param onEvent - Callback invoked for each parsed `StreamEvent` received from the stream
 *
 * @throws Error if the HTTP response is not OK or the response body is missing
 */
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
