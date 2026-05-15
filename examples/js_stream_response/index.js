serve(async function() {
    const { response, stream } = http.stream({
        headers: {
            "Content-Type": "text/plain; charset=utf-8",
            "Cache-Control": "no-store",
        },
    });

    void (async () => {
        for (const line of ["chunk-1", "chunk-2", "chunk-3"]) {
            await stream.write(line + "\n");
        }
        await stream.close();
    })();

    return response;
});
