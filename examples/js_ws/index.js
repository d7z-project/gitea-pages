serve(function(request) {
    const { socket, response } = upgradeWebSocket(request);
    socket.addEventListener("message", async (event) => {
        await socket.send("ECHO: " + event.data);
    });
    return response;
});
