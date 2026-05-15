serve(function(request) {
    const name = new URL(request.url).searchParams.get("name")?.trim();
    if (!name) throw new Error("Missing or empty name parameter");

    const { socket, response } = upgradeWebSocket(request);

    const eventPull = async () => {
        while (true) await socket.send(await event.load("messages"));
    };

    socket.addEventListener("message", async (evt) => {
        const data = typeof evt.data === "string" ? evt.data : "";
        const message = data.trim();
        if (message) {
            await event.put("messages", JSON.stringify({ name, data: message }));
        }
    });

    void eventPull();
    return response;
});
