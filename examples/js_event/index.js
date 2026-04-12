function sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}

serve(async function() {
    console.log("boot");
    for (const ms of [0, 1000, 2000, 3000, 4000, 5000]) {
        if (ms > 0) {
            await sleep(1000);
        }
        console.log(ms);
    }
    console.log("boot end");
    return new Response("event loop demo finished");
});
