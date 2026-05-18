const app = http.router();
const workspaceName = "workspace";
const root = storage.child(workspaceName);

async function ensureWorkspace() {
    await storage.mkdir(workspaceName, { recursive: true });
}

function cleanPath(raw, allowRoot = true) {
    const value = String(raw || "").replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
    if (!value) {
        if (allowRoot) {
            return "";
        }
        throw new Error("path is required");
    }
    const parts = value.split("/");
    for (const part of parts) {
        if (!part || part === "." || part === "..") {
            throw new Error("invalid path");
        }
    }
    return parts.join("/");
}

function splitParent(target) {
    const normalized = cleanPath(target, false);
    const index = normalized.lastIndexOf("/");
    if (index < 0) {
        return { parent: "", name: normalized };
    }
    return {
        parent: normalized.slice(0, index),
        name: normalized.slice(index + 1),
    };
}

async function listDirectory(target) {
    await ensureWorkspace();
    const current = cleanPath(target, true);
    const currentDir = current ? root.child(current) : root;
    const entries = await currentDir.readdir(".", { withFileTypes: true });
    const items = [];

    for (const entry of entries) {
        const fullPath = current ? `${current}/${entry.name}` : entry.name;
        const stat = await root.stat(fullPath);
        items.push({
            name: entry.name,
            path: fullPath,
            type: entry.isDirectory() ? "dir" : "file",
            size: stat.size,
            modTime: stat.modTime,
        });
    }

    items.sort((a, b) => {
        if (a.type !== b.type) {
            return a.type === "dir" ? -1 : 1;
        }
        return a.name.localeCompare(b.name);
    });

    return {
        path: current,
        parent: current.includes("/") ? current.slice(0, current.lastIndexOf("/")) : "",
        items,
    };
}

app.get("/files/config", function() {
    return http.json({
        maxRequestBodyBytes: page.limits.maxRequestBodyBytes,
    });
});

app.get("/files/list", async function(request) {
    const target = new URL(request.url).searchParams.get("path") || "";
    return http.json(await listDirectory(target));
});

app.post("/files/directories", async function(request) {
    await ensureWorkspace();
    const body = await request.json();
    const base = cleanPath(body?.path, true);
    const name = cleanPath(body?.name, false);
    const fullPath = base ? `${base}/${name}` : name;
    await root.mkdir(fullPath, { recursive: true });
    return http.json(await listDirectory(base), { status: 201 });
});

app.delete("/files/directories", async function(request) {
    const target = new URL(request.url).searchParams.get("path") || "";
    const current = cleanPath(target, false);
    const { parent } = splitParent(current);
    await root.rm(current, { recursive: true, force: true });
    return http.json(await listDirectory(parent));
});

app.post("/files/upload", async function(request) {
    await ensureWorkspace();
    const url = new URL(request.url);
    const base = cleanPath(url.searchParams.get("path"), true);
    const rawName = url.searchParams.get("name") || request.headers.get("x-file-name");
    const name = cleanPath(rawName, false);
    const fullPath = base ? `${base}/${name}` : name;
    const body = await request.bytes();
    await root.writeFile(fullPath, body, { mkdir: true });
    return http.json(await listDirectory(base), { status: 201 });
});

app.get("/files/download", async function(request) {
    const target = cleanPath(new URL(request.url).searchParams.get("path"), false);
    const body = await root.readFile(target);
    const { name } = splitParent(target);
    return new Response(body, {
        headers: {
            "content-type": "application/octet-stream",
            "content-disposition": `attachment; filename="${name}"`,
        },
    });
});

app.delete("/files/file", async function(request) {
    const target = cleanPath(new URL(request.url).searchParams.get("path"), false);
    const { parent } = splitParent(target);
    await root.unlink(target);
    return http.json(await listDirectory(parent));
});

app.post("/files/rename", async function(request) {
    const body = await request.json();
    const source = cleanPath(body?.path, false);
    const { parent } = splitParent(source);
    const nextName = cleanPath(body?.name, false);
    const target = parent ? `${parent}/${nextName}` : nextName;
    await root.rename(source, target);
    return http.json(await listDirectory(parent));
});

serve(app);
