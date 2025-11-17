function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}
console.log('boot');
(async () => {
    console.log(0);
    await sleep(1000);
    console.log(1000);
    await sleep(1000);
    console.log(2000);
    await sleep(1000);
    console.log(3000);
    await sleep(1000);
    console.log(4000);
    await sleep(1000);
    console.log(5000);
})();
console.log('boot end');