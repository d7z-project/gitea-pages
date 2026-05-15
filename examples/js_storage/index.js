serve(async function(request) {
    const pathname = new URL(request.url).pathname;

    if (pathname.endsWith("/write")) {
        const current = await storage.exists("notes/hello.txt")
            ? await storage.readFile("notes/hello.txt", "utf8")
            : "";
        const next = current ? current + "\nupdated" : "created";
        await storage.writeFile("notes/hello.txt", next, { mkdir: true });
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
