serve(async function() {
    const entries = fs.list().map((item) => item.name).sort();
    const reader = fs.openReadable("sample.txt");
    const chunks = [];

    while (true) {
        const part = await reader.read({ size: 5 });
        if (part.done) {
            break;
        }
        chunks.push(new TextDecoder().decode(part.value));
    }

    await reader.close();

    return Response.json({
        entries,
        content: chunks.join(""),
    });
});
