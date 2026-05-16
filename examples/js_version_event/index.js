const app = http.router();

app.get("/version/stream", async function() {
    const { stream, response } = http.sse();

    void (async function() {
        try {
            while (!stream.closed) {
                const message = await versionEvent.load("messages");
                if (stream.closed) {
                    break;
                }
                await stream.send(message, { event: "message" });
            }
        } catch (_) {
            if (!stream.closed) {
                stream.close();
            }
        }
    })();

    return response;
});

app.post("/version/publish", async function(request) {
    const form = await http.read(request, "form");
    const message = typeof form.message === "string" ? form.message.trim() : "";
    if (!message) {
        return http.error(400, "message is required");
    }

    await versionEvent.put("messages", JSON.stringify({
        message,
        commit: page.meta.commit.slice(0, 7),
        at: new Date().toISOString(),
    }));
    return http.noContent();
});

serve(app);
