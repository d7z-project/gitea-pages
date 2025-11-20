(async ()=>{
    let ws = websocket();
    let shouldExit = false;
    while (!shouldExit) {
        let data = await ws.readText();
        switch (data) {
            case "exit":
                shouldExit = true;
                break;
            case "panic":
                throw Error("错误");
            case "date":
                ws.writeText(new Date().toJSON())
                break
            default:
                ws.writeText("收到信息:" + data)
                break;
        }
    }
})()