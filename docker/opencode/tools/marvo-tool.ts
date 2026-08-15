function setting(name: string) {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`Marvo tool environment is missing ${name}`);
  return value;
}

export async function callMarvo(tool: string, body: unknown) {
  const base = setting("MARVO_TOOL_URL");
  const token = setting("MARVO_TOOL_TOKEN");
  const response = await fetch(`${base}/${tool}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Marvo-Tool-Token": token,
    },
    body: JSON.stringify(body),
  });
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    const message =
      payload && typeof payload.error === "string"
        ? payload.error
        : `HTTP ${response.status}`;
    throw new Error(`Marvo operation failed: ${message}`);
  }
  return JSON.stringify(payload);
}
