const name = (request.getQuery("name"))?.trim();

if (!name) {
    throw new Error(`Missing or empty name parameter`);
}

const ws = websocket();

async function eventPull() {
    while (true) {
        const data  = await event.load('messages')
        await ws.writeText(data);
    }
}
async function messagePull() {
    while (true) {
        const data  = await ws.readText()
        if (data === "exit")
            await event.put("messages", JSON.stringify({
                name:name,
                data: name+' 已断开连接'
            }));
            break;
        if (data?.trim()) {
            await event.put("messages", JSON.stringify({
                name:name,
                data: data.trim()
            }));
        }
    }
}

(async () => {
    await Promise.any([eventPull(), messagePull()])
})()