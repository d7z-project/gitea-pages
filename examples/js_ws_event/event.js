serve(function(request) {
    const name = new URL(request.url).searchParams.get("name")?.trim();
    if (!name) throw new Error("Missing or empty name parameter");

    const { socket, response } = upgradeWebSocket(request);

    const eventPull = async () => {
        while (true) await socket.send(await event.load("messages"));
    };

    socket.addEventListener("message", async (evt) => {
        const data = typeof evt.data === "string" ? evt.data : "";
        if (data?.trim()) {
            await event.put("messages", JSON.stringify({
                name,
                data: data === "exit" ? `${name} 已断开连接` : data.trim(),
            }));
        }
        if (data === "exit") {
            socket.close();
        }
    });

    void eventPull();
    return response;
});
