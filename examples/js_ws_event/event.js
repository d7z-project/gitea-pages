const name = request.getQuery("name")?.trim();
if (!name) throw new Error('Missing or empty name parameter');

const ws = websocket();

const eventPull = async () => {
    while (true) await ws.writeText(await event.load('messages'));
};

const messagePull = async () => {
    while (true) {
        const data = await ws.readText();
        if (data?.trim()) {
            await event.put("messages", JSON.stringify({
                name: name,
                data: data === "exit" ? `${name} 已断开连接` : data.trim()
            }));
        }
        if (data === "exit") break;
    }
};
(async () => await Promise.any([eventPull(), messagePull()]))();