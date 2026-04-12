serve(function() {
    const { stream, response } = http.sse();

    void (async () => {
        for (let i = 1; i <= 5; i++) {
            await stream.send(JSON.stringify({
                id: i,
                time: new Date().toISOString(),
                message: `tick-${i}`,
            }), {
                event: "message",
                id: String(i),
            });
        }
        stream.close();
    })();

    return response;
});
