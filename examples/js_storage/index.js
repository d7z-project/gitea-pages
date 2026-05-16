serve(async function(request) {
    const pathname = new URL(request.url).pathname;
    const notes = storage.child("notes");

    if (pathname.endsWith("/write")) {
        const current = await notes.exists("hello.txt")
            ? await notes.readFile("hello.txt", "utf8")
            : "";
        const next = current ? current + "\nupdated" : "created";
        await notes.writeFile("hello.txt", next, { mkdir: true });
    }

    if (pathname.endsWith("/manage")) {
        for (const name of ["draft.txt", "draft.copy.txt", "draft-final.txt"]) {
            await notes.rm(name, { force: true });
        }
        await notes.writeFile("draft.txt", "draft", { mkdir: true });
        await notes.copyFile("draft.txt", "draft.copy.txt");
        await notes.rename("draft.copy.txt", "draft-final.txt");

        const stat = await notes.stat("draft-final.txt");
        const entries = await notes.readdir(undefined, { withFileTypes: true });

        await notes.rm("draft.txt", { force: true });
        await notes.rm("draft-final.txt", { force: true });

        return Response.json({
            stat: {
                name: stat.name,
                path: stat.path,
                size: stat.size,
                mode: stat.mode,
                modTime: stat.modTime,
                isFile: stat.isFile(),
                isDirectory: stat.isDirectory(),
            },
            entries: entries.map((entry) => ({
                name: entry.name,
                path: entry.path,
                isFile: entry.isFile(),
                isDirectory: entry.isDirectory(),
            })),
        });
    }

    if (pathname.endsWith("/stream")) {
        const writer = notes.openWritable("stream.txt", { mkdir: true });
        await writer.write("first line\n");
        await writer.write("second line\n");
        await writer.close();

        const reader = notes.openReadable("stream.txt", { offset: 6 });
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

    const exists = await notes.exists("hello.txt");
    const content = exists ? await notes.readFile("hello.txt", "utf8") : "";
    const files = exists ? notes.readdirSync().sort() : [];
    const stat = exists ? notes.statSync("hello.txt") : null;

    return Response.json({
        exists,
        content,
        files,
        stat: stat ? {
            name: stat.name,
            path: stat.path,
            size: stat.size,
            mode: stat.mode,
            modTime: stat.modTime,
            isFile: stat.isFile(),
            isDirectory: stat.isDirectory(),
        } : null,
    });
});
