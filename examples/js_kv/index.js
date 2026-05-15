serve(async function(request) {
    const db = kv.repo("self");
    const pathname = new URL(request.url).pathname;

    if (pathname.endsWith("/put")) {
        const current = db.get("key");
        const next = current == null ? 0 : parseInt(current, 10) + 1;
        db.set("key", String(next));
    }

    return new Response("current value: " + (db.get("key") ?? "0"));
});
