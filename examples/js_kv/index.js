serve(async function(request) {
    const db = kv.repo("self");
    const test = kv.repo("test");
    const pathname = new URL(request.url).pathname;

    if (pathname.endsWith("/put")) {
        const current = db.get("key");
        if (current == null) {
            db.set("key", "0");
        } else {
            db.set("key", (parseInt(current, 10) + 1).toString());
        }
    }

    for (let i = 0; i < 500; i++) {
        test.set("key" + i, "value" + i);
    }

    const list = test.list();
    console.log(list.keys.length);
    console.log(list.cursor);
    console.log(list.hasNext);

    return new Response("当前存储的数值为 " + (db.get("key") ?? "0"));
});
