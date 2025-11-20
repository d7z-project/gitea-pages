const name = (await request.getQuery("name"))?.trim();

if (!name) {
    throw new Error(`Missing or empty name parameter`);
}

const ws = websocket();

try {
    // 事件处理
    event.subscribe("messages").on((msg) => {
        ws.writeText(msg);
    });

    // 主循环
    for await (const data of ws.readText()) {
        if (data === "exit") break;

        if (data?.trim()) {
            await event.put("messages", JSON.stringify({
                name,
                data: data.trim()
            }));
        }
    }
} finally {
    ws.close();
}