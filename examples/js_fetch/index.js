serve(async function(request) {
    const target = new URL("/assets/hello.txt", request.url).toString();
    const outbound = new Request(target, {
        method: "GET",
        headers: {
            "x-demo": "fetch-example",
        },
    });
    const upstream = await fetch(outbound);
    const bytes = await upstream.bytes();

    const blob = await Response.json({
        target,
        ok: true,
    }).blob();

    return Response.json({
        target,
        status: upstream.status,
        contentType: upstream.headers.get("content-type"),
        text: new TextDecoder().decode(bytes),
        blobText: await blob.text(),
        blobBytes: Array.from(await blob.bytes()),
    });
});
