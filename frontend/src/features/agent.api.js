/**
 * Sends a prompt to the agent service and consumes its Server-Sent Events.
 *
 * EventSource only supports GET, so this uses fetch() with a streaming body
 * reader and parses the "event: x / data: {...}" frames by hand.
 *
 * @param {FormData} formData  prompt, conversationId, agent and optional file
 * @param {(event: string, data: object) => void} onEvent
 * @param {AbortSignal} signal  aborts the request (the Stop button)
 */
export async function streamPrompt(formData, onEvent, signal) {
  const res = await fetch("/api/agent/chat", {
    method: "POST",
    body: formData,
    credentials: "same-origin",
    headers: { "X-Requested-With": "XMLHttpRequest" },
    signal,
  });

  if (!res.ok) {
    let body = {};
    try {
      body = await res.json();
    } catch {
      /* non-JSON error page */
    }
    onEvent("error", {
      status: res.status,
      code: body.code || "http_error",
      title: body.title || (res.status === 429 ? "Slow down" : "Something went wrong"),
      message: body.message || "Please try again.",
    });
    return;
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let sep;
    while ((sep = buffer.indexOf("\n\n")) !== -1) {
      const frame = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      let event = "message";
      let data = "";
      for (const line of frame.split("\n")) {
        if (line.startsWith("event: ")) event = line.slice(7);
        else if (line.startsWith("data: ")) data += line.slice(6);
      }
      if (data) onEvent(event, JSON.parse(data));
    }
  }
}
