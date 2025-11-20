const name = (request.getQuery("name"))?.trim();

if (!name) {
    throw new Error(`Missing or empty name parameter`);
}

const ws = websocket();

async function eventPull() {
    while (true) {
        const data  = await event.pull('messages')
        ws.writeText(data);
    }
}

async function messagePull() {
    while (true) {
        const data  = await ws.readText()
        if (data === "exit") break;
        if (data?.trim()) {
            await event.put("messages", JSON.stringify({
                name:name,
                data: data.trim()
            }));
        }
    }
}

(async () => {
    await Promise.all(eventPull(), messagePull())
})()