declare global {
    interface Blob {
        bytes(): Promise<Uint8Array>;
    }

    interface Request {
        bytes(): Promise<Uint8Array>;
        readonly ip: string;
        readonly RemoteIP: string;
    }

    interface Response {
        bytes(): Promise<Uint8Array>;
    }

    type RequestHandler = (request: Request) => Response | Promise<Response>;

    interface FetchHandlerObject {
        fetch(request: Request): Response | Promise<Response>;
    }

    type PageHandler = RequestHandler | FetchHandlerObject;

    function serve(handler: PageHandler): void;

    interface RouteContext {
        params: Record<string, string>;
        query: URLSearchParams;
        url: URL;
    }

    type RouteHandler = (request: Request, ctx: RouteContext) => Response | Promise<Response>;

    interface Router {
        on(method: string, path: string, handler: RouteHandler): Router;
        all(path: string, handler: RouteHandler): Router;
        get(path: string, handler: RouteHandler): Router;
        post(path: string, handler: RouteHandler): Router;
        put(path: string, handler: RouteHandler): Router;
        patch(path: string, handler: RouteHandler): Router;
        delete(path: string, handler: RouteHandler): Router;
        options(path: string, handler: RouteHandler): Router;
        head(path: string, handler: RouteHandler): Router;
        fetch(request: Request): Promise<Response>;
        handle(request: Request): Promise<Response>;
    }

    interface HttpHelpers {
        text(body: string, init?: ResponseInit): Response;
        html(body: string, init?: ResponseInit): Response;
        json(data: any, init?: ResponseInit): Response;
        read<T = any>(request: Request, kind?: "json" | "form" | "text" | "bytes" | string): Promise<T | string | ArrayBuffer | Record<string, string>>;
        redirect(location: string, status?: number): Response;
        error(status: number, message?: string): Response;
        noContent(): Response;
        notFound(message?: string): Response;
        methodNotAllowed(methods?: string | string[]): Response;
        cookie(request: Request, name: string): string | null;
        cookie(request: Request): Record<string, string>;
        withHeaders(response: Response, headers: Record<string, string>): Promise<Response>;
        setCookie(response: Response, name: string, value: string, options?: {
            maxAge?: number;
            domain?: string;
            path?: string;
            expires?: string;
            sameSite?: string;
            httpOnly?: boolean;
            secure?: boolean;
        }): Promise<Response>;
        clearCookie(response: Response, name: string, options?: {
            domain?: string;
            path?: string;
            sameSite?: string;
            httpOnly?: boolean;
            secure?: boolean;
        }): Promise<Response>;
        sse(): {
            stream: EventStream;
            response: Response;
        };
        stream(init?: ResponseInit): {
            stream: WritableByteStream;
            response: Response;
        };
        router(): Router;
    }

    const http: HttpHelpers;

    interface PageMeta {
        org: string;
        repo: string;
        commit: string;
    }

    interface PageAuthIdentity {
        subject: string;
        name: string;
    }

    interface PageAuth {
        authenticated: boolean;
        identity: PageAuthIdentity | null;
    }

    interface PageEntry {
        name: string;
        path: string;
        type: "file" | "dir" | "symlink" | "submodule";
        size?: number;
    }

    interface PageFS {
        list(path?: string): PageEntry[];
        read(path: string): Promise<Uint8Array>;
        readText(path: string): Promise<string>;
        readSync(path: string): Uint8Array;
        readTextSync(path: string): string;
        openReadable(path: string, options?: { offset?: number }): ReadableByteStream;
    }

    interface ReadableByteStreamReadResult {
        done: boolean;
        value?: Uint8Array;
    }

    interface ReadableByteStream {
        read(options?: { size?: number }): Promise<ReadableByteStreamReadResult>;
        cancel(reason?: string): Promise<void>;
        close(): Promise<void>;
        readonly closed: boolean;
    }

    interface WritableByteStream {
        ready(): Promise<void>;
        write(chunk: string | Uint8Array | ArrayBuffer): Promise<void>;
        flush(): Promise<void>;
        close(): Promise<void>;
        abort(reason?: string): Promise<void>;
        readonly closed: boolean;
    }

    type StorageEncoding = "utf8";

    interface StorageStat {
        name: string;
        path: string;
        size: number;
        mode?: number;
        modTime?: string;
        isFile(): boolean;
        isDirectory(): boolean;
    }

    interface StorageDirent {
        name: string;
        path: string;
        isFile(): boolean;
        isDirectory(): boolean;
    }

    interface StorageReadFileOptions {
        encoding?: StorageEncoding;
    }

    interface StorageWriteFileOptions {
        encoding?: StorageEncoding;
        mode?: number;
        mkdir?: boolean;
        create?: boolean;
        truncate?: boolean;
    }

    interface StorageMkdirOptions {
        recursive?: boolean;
        mode?: number;
    }

    interface StorageReaddirOptions {
        withFileTypes?: boolean;
        recursive?: boolean;
    }

    interface StorageRmOptions {
        recursive?: boolean;
        force?: boolean;
    }

    interface StorageRenameOptions {
        overwrite?: boolean;
    }

    interface StorageNamespace {
        child(...paths: string[]): StorageNamespace;
        access(path: string): Promise<void>;
        accessSync(path: string): void;
        exists(path: string): Promise<boolean>;
        existsSync(path: string): boolean;
        stat(path: string): Promise<StorageStat>;
        statSync(path: string): StorageStat;
        lstat(path: string): Promise<StorageStat>;
        lstatSync(path: string): StorageStat;
        readdir(path?: string, options?: StorageReaddirOptions): Promise<string[] | StorageDirent[]>;
        readdirSync(path?: string, options?: StorageReaddirOptions): string[] | StorageDirent[];
        openReadable(path: string, options?: { offset?: number }): ReadableByteStream;
        openWritable(path: string, options?: Omit<StorageWriteFileOptions, "encoding">): WritableByteStream;
        readFile(path: string, options?: StorageReadFileOptions | StorageEncoding): Promise<Uint8Array | string>;
        readFileSync(path: string, options?: StorageReadFileOptions | StorageEncoding): Uint8Array | string;
        writeFile(path: string, data: string | Uint8Array | ArrayBuffer, options?: StorageWriteFileOptions): Promise<void>;
        writeFileSync(path: string, data: string | Uint8Array | ArrayBuffer, options?: StorageWriteFileOptions): void;
        mkdir(path: string, options?: StorageMkdirOptions): Promise<void>;
        mkdirSync(path: string, options?: StorageMkdirOptions): void;
        rm(path: string, options?: StorageRmOptions): Promise<void>;
        rmSync(path: string, options?: StorageRmOptions): void;
        unlink(path: string): Promise<void>;
        unlinkSync(path: string): void;
        rename(oldPath: string, newPath: string, options?: StorageRenameOptions): Promise<void>;
        renameSync(oldPath: string, newPath: string, options?: StorageRenameOptions): void;
        copyFile(src: string, dest: string): Promise<void>;
        copyFileSync(src: string, dest: string): void;
    }

    interface KVListResult {
        keys: string[];
        items: { key: string; value: string }[];
        cursor: string;
        hasNext: boolean;
    }

    interface KVOps {
        get(key: string): string | null;
        set(key: string, value: string, ttl?: number): void;
        delete(key: string): boolean;
        putIfNotExists(key: string, value: string, ttl?: number): boolean;
        compareAndSwap(key: string, oldValue: string, newValue: string): boolean;
        list(limit?: number, cursor?: string): KVListResult;
    }

    interface KVSystem {
        repo(...group: string[]): KVOps;
        org(...group: string[]): KVOps;
    }

    interface EventSystem {
        /**
         * Waits for the next broadcast event on the given key.
         *
         * This is a live event stream, not a key/value read and not a history API.
         * If the local event backlog overflows, the promise rejects with
         * `"event backlog overflow"`. A later call can establish a fresh
         * subscription and continue receiving new events.
         */
        load(key: string): Promise<any>;
        /**
         * Broadcasts one event value to the given key.
         */
        put(key: string, value: string): Promise<void>;
    }

    interface PageHost {
        meta: PageMeta;
        auth: PageAuth;
    }

    const page: PageHost;
    /**
     * Read-only page source files from the current commit.
     */
    const fs: PageFS;
    /**
     * Read-write repo-scoped storage isolated to the current repo.
     */
    const storage: StorageNamespace;
    const kv: KVSystem;
    /**
     * Broadcast events shared across page versions in the same repo.
     */
    // @ts-ignore
    const event: EventSystem;
    /**
     * Broadcast events scoped to the current page commit.
     */
    // @ts-ignore
    const versionEvent: EventSystem;

    interface PageWebSocket extends WebSocket {
        /**
         * Sends text or binary data. Typed-array views are sent using their
         * actual byte window, not the entire backing ArrayBuffer.
         */
        send(data: string | Uint8Array | ArrayBuffer): Promise<void>;
    }

    function upgradeWebSocket(request?: Request): {
        socket: PageWebSocket;
        response: Response;
    };

    interface EventStream {
        send(data: string, options?: {
            event?: string;
            id?: string;
            retry?: number;
        }): Promise<void>;
        close(): void;
        readonly closed: boolean;
    }
}

export {};
