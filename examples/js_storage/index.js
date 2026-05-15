serve(async function(request) {
    const pathname = new URL(request.url).pathname;

    if (pathname.endsWith("/write")) {
        const current = await storage.exists("notes/hello.txt")
            ? await storage.readFile("notes/hello.txt", "utf8")
            : "";
        const next = current ? current + "\nupdated" : "created";
        await storage.writeFile("notes/hello.txt", next, { mkdir: true });
    }

    if (pathname.endsWith("/stream")) {
        const writer = storage.openWritable("notes/stream.txt", { mkdir: true });
        await writer.write("first line\n");
        await writer.write("second line\n");
        await writer.close();

        const reader = storage.openReadable("notes/stream.txt", { offset: 6 });
        const chunks = [];
        while (true) {
            const part = await reader.read({ size: 8 });
            if (part.done) {
                break;
            }
            chunks.push(new TextDecoder().decode(part.value));
        }
        await reader.close();

        return Response.json({
            path: "notes/stream.txt",
            fromOffset: chunks.join(""),
        });
    }

    const exists = await storage.exists("notes/hello.txt");
    const content = exists ? await storage.readFile("notes/hello.txt", "utf8") : "";
    const files = exists ? storage.readdirSync("notes").sort() : [];

    return Response.json({
        exists,
        content,
        files,
    });
});
