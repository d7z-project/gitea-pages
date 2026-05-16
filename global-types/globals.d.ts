declare global {
    interface Blob {
        /**
         * Reads the blob payload as a Uint8Array.
         */
        bytes(): Promise<Uint8Array>;
    }

    interface Request {
        /**
         * Reads the request body as a Uint8Array.
         */
        bytes(): Promise<Uint8Array>;
        /**
         * Resolved client IP after trusted-proxy processing.
         */
        readonly ip: string;
        /**
         * Alias of {@link Request.ip} kept for compatibility.
         */
        readonly RemoteIP: string;
    }

    interface Response {
        /**
         * Reads the response body as a Uint8Array.
         */
        bytes(): Promise<Uint8Array>;
    }

    /**
     * Basic page handler function registered through {@link serve}.
     */
    type RequestHandler = (request: Request) => Response | Promise<Response>;

    /**
     * Object-style page handler that exposes a fetch method.
     */
    interface FetchHandlerObject {
        fetch(request: Request): Response | Promise<Response>;
    }

    /**
     * Supported handler shapes for the goja page runtime.
     */
    type PageHandler = RequestHandler | FetchHandlerObject;

    /**
     * Registers the page entry handler for the current script.
     */
    function serve(handler: PageHandler): void;

    /**
     * Route match context passed to handlers created with {@link http.router}.
     */
    interface RouteContext {
        /**
         * Named params extracted from `:name` segments in the route pattern.
         */
        params: Record<string, string>;
        /**
         * Parsed query string of the current request URL.
         */
        query: URLSearchParams;
        /**
         * Parsed current request URL.
         */
        url: URL;
    }

    /**
     * Router handler with access to route params and parsed URL data.
     */
    type RouteHandler = (request: Request, ctx: RouteContext) => Response | Promise<Response>;

    /**
     * Lightweight router helper for goja handlers.
     */
    interface Router {
        /**
         * Registers a route for a specific HTTP method.
         */
        on(method: string, path: string, handler: RouteHandler): Router;
        /**
         * Registers a route for any HTTP method.
         */
        all(path: string, handler: RouteHandler): Router;
        get(path: string, handler: RouteHandler): Router;
        post(path: string, handler: RouteHandler): Router;
        put(path: string, handler: RouteHandler): Router;
        patch(path: string, handler: RouteHandler): Router;
        delete(path: string, handler: RouteHandler): Router;
        options(path: string, handler: RouteHandler): Router;
        head(path: string, handler: RouteHandler): Router;
        /**
         * Dispatches a request through the registered routes.
         */
        fetch(request: Request): Promise<Response>;
        /**
         * Alias of {@link Router.fetch}.
         */
        handle(request: Request): Promise<Response>;
    }

    /**
     * Convenience helpers exposed as the global {@link http} object.
     */
    interface HttpHelpers {
        /**
         * Creates a plain-text response with UTF-8 content type.
         */
        text(body: string, init?: ResponseInit): Response;
        /**
         * Creates an HTML response with UTF-8 content type.
         */
        html(body: string, init?: ResponseInit): Response;
        /**
         * Creates a JSON response.
         */
        json(data: any, init?: ResponseInit): Response;
        /**
         * Reads and parses the request body using a simple mode hint.
         */
        read<T = any>(request: Request, kind?: "json" | "form" | "text" | "bytes" | string): Promise<T | string | ArrayBuffer | Record<string, string>>;
        /**
         * Creates a redirect response.
         */
        redirect(location: string, status?: number): Response;
        /**
         * Creates a text error response with the given HTTP status.
         */
        error(status: number, message?: string): Response;
        /**
         * Creates an empty 204 response.
         */
        noContent(): Response;
        /**
         * Creates a 404 response.
         */
        notFound(message?: string): Response;
        /**
         * Creates a 405 response and optionally sets the Allow header.
         */
        methodNotAllowed(methods?: string | string[]): Response;
        /**
         * Reads one cookie value from the request.
         */
        cookie(request: Request, name: string): string | null;
        /**
         * Reads all cookies from the request into a key/value object.
         */
        cookie(request: Request): Record<string, string>;
        /**
         * Clones a response and merges extra headers into it.
         */
        withHeaders(response: Response, headers: Record<string, string>): Promise<Response>;
        /**
         * Clones a response and appends a Set-Cookie header.
         */
        setCookie(response: Response, name: string, value: string, options?: {
            maxAge?: number;
            domain?: string;
            path?: string;
            expires?: string;
            sameSite?: string;
            httpOnly?: boolean;
            secure?: boolean;
        }): Promise<Response>;
        /**
         * Clones a response and clears a cookie.
         */
        clearCookie(response: Response, name: string, options?: {
            domain?: string;
            path?: string;
            sameSite?: string;
            httpOnly?: boolean;
            secure?: boolean;
        }): Promise<Response>;
        /**
         * Creates an SSE response pair.
         */
        sse(): {
            stream: EventStream;
            response: Response;
        };
        /**
         * Creates a manually writable streaming response pair.
         */
        stream(init?: ResponseInit): {
            stream: WritableByteStream;
            response: Response;
        };
        /**
         * Creates a small in-process router.
         */
        router(): Router;
    }

    /**
     * Global helper collection for common HTTP response patterns.
     */
    const http: HttpHelpers;

    /**
     * Current page identity.
     */
    interface PageMeta {
        org: string;
        repo: string;
        commit: string;
    }

    /**
     * Authenticated user identity exposed to page scripts.
     */
    interface PageAuthIdentity {
        subject: string;
        name: string;
    }

    /**
     * Current authentication state of the request.
     */
    interface PageAuth {
        authenticated: boolean;
        identity: PageAuthIdentity | null;
    }

    /**
     * Read-only directory entry from the page source tree.
     */
    interface PageEntry {
        name: string;
        path: string;
        type: "file" | "dir" | "symlink" | "submodule";
        size?: number;
    }

    /**
     * Read-only filesystem view of the current page commit.
     */
    interface PageFS {
        /**
         * Lists entries under a page source directory.
         */
        list(path?: string): PageEntry[];
        /**
         * Reads a source file as bytes.
         */
        read(path: string): Promise<Uint8Array>;
        /**
         * Reads a source file as text.
         */
        readText(path: string): Promise<string>;
        /**
         * Reads a source file as bytes synchronously.
         */
        readSync(path: string): Uint8Array;
        /**
         * Reads a source file as text synchronously.
         */
        readTextSync(path: string): string;
        /**
         * Opens a readable byte stream for a source file.
         */
        openReadable(path: string, options?: { offset?: number }): ReadableByteStream;
    }

    /**
     * One chunk returned by {@link ReadableByteStream.read}.
     */
    interface ReadableByteStreamReadResult {
        done: boolean;
        value?: Uint8Array;
    }

    /**
     * Minimal readable byte stream used by page and storage file APIs.
     */
    interface ReadableByteStream {
        /**
         * Reads the next chunk from the stream.
         */
        read(options?: { size?: number }): Promise<ReadableByteStreamReadResult>;
        /**
         * Cancels and closes the stream.
         */
        cancel(reason?: string): Promise<void>;
        /**
         * Closes the stream.
         */
        close(): Promise<void>;
        /**
         * Whether the stream has been closed.
         */
        readonly closed: boolean;
    }

    /**
     * Minimal writable byte stream used by storage and response streaming APIs.
     */
    interface WritableByteStream {
        /**
         * Resolves when the stream has actually taken over the response output.
         */
        ready(): Promise<void>;
        /**
         * Writes a text or binary chunk to the stream.
         */
        write(chunk: string | Uint8Array | ArrayBuffer): Promise<void>;
        /**
         * Flushes buffered output when supported.
         */
        flush(): Promise<void>;
        /**
         * Closes the stream.
         */
        close(): Promise<void>;
        /**
         * Aborts the stream and closes it.
         */
        abort(reason?: string): Promise<void>;
        /**
         * Whether the stream has been closed.
         */
        readonly closed: boolean;
    }

    /**
     * Supported text encoding options for storage file reads and writes.
     */
    type StorageEncoding = "utf8";

    /**
     * File metadata returned by storage stat operations.
     */
    interface StorageStat {
        name: string;
        path: string;
        size: number;
        mode?: number;
        modTime?: string;
        isFile(): boolean;
        isDirectory(): boolean;
    }

    /**
     * Directory entry returned by storage directory listing operations.
     */
    interface StorageDirent {
        name: string;
        path: string;
        isFile(): boolean;
        isDirectory(): boolean;
    }

    /**
     * Options for storage file reads.
     */
    interface StorageReadFileOptions {
        encoding?: StorageEncoding;
    }

    /**
     * Options for storage file writes.
     */
    interface StorageWriteFileOptions {
        encoding?: StorageEncoding;
        mode?: number;
        mkdir?: boolean;
        create?: boolean;
        truncate?: boolean;
    }

    /**
     * Options for storage mkdir operations.
     */
    interface StorageMkdirOptions {
        recursive?: boolean;
        mode?: number;
    }

    /**
     * Options for storage directory listing.
     */
    interface StorageReaddirOptions {
        withFileTypes?: boolean;
        recursive?: boolean;
    }

    /**
     * Options for storage remove operations.
     */
    interface StorageRmOptions {
        recursive?: boolean;
        force?: boolean;
    }

    /**
     * Options for storage rename operations.
     */
    interface StorageRenameOptions {
        overwrite?: boolean;
    }

    /**
     * Repo-scoped read-write storage namespace.
     */
    interface StorageNamespace {
        /**
         * Creates a child namespace rooted under the given sub-path.
         */
        child(...paths: string[]): StorageNamespace;
        /**
         * Verifies that a path exists.
         */
        access(path: string): Promise<void>;
        /**
         * Synchronous variant of {@link StorageNamespace.access}.
         */
        accessSync(path: string): void;
        /**
         * Checks whether a path exists.
         */
        exists(path: string): Promise<boolean>;
        /**
         * Synchronous variant of {@link StorageNamespace.exists}.
         */
        existsSync(path: string): boolean;
        /**
         * Reads file metadata.
         */
        stat(path: string): Promise<StorageStat>;
        /**
         * Synchronous variant of {@link StorageNamespace.stat}.
         */
        statSync(path: string): StorageStat;
        /**
         * Alias of {@link StorageNamespace.stat}.
         */
        lstat(path: string): Promise<StorageStat>;
        /**
         * Synchronous alias of {@link StorageNamespace.statSync}.
         */
        lstatSync(path: string): StorageStat;
        /**
         * Lists entries under a directory.
         */
        readdir(path?: string, options?: StorageReaddirOptions): Promise<string[] | StorageDirent[]>;
        /**
         * Synchronous variant of {@link StorageNamespace.readdir}.
         */
        readdirSync(path?: string, options?: StorageReaddirOptions): string[] | StorageDirent[];
        /**
         * Opens a file for streaming reads.
         */
        openReadable(path: string, options?: { offset?: number }): ReadableByteStream;
        /**
         * Opens a file for streaming writes.
         */
        openWritable(path: string, options?: Omit<StorageWriteFileOptions, "encoding">): WritableByteStream;
        /**
         * Reads a full file into memory.
         */
        readFile(path: string, options?: StorageReadFileOptions | StorageEncoding): Promise<Uint8Array | string>;
        /**
         * Synchronous variant of {@link StorageNamespace.readFile}.
         */
        readFileSync(path: string, options?: StorageReadFileOptions | StorageEncoding): Uint8Array | string;
        /**
         * Writes a full file from memory.
         */
        writeFile(path: string, data: string | Uint8Array | ArrayBuffer, options?: StorageWriteFileOptions): Promise<void>;
        /**
         * Synchronous variant of {@link StorageNamespace.writeFile}.
         */
        writeFileSync(path: string, data: string | Uint8Array | ArrayBuffer, options?: StorageWriteFileOptions): void;
        /**
         * Creates a directory.
         */
        mkdir(path: string, options?: StorageMkdirOptions): Promise<void>;
        /**
         * Synchronous variant of {@link StorageNamespace.mkdir}.
         */
        mkdirSync(path: string, options?: StorageMkdirOptions): void;
        /**
         * Removes a file or directory.
         */
        rm(path: string, options?: StorageRmOptions): Promise<void>;
        /**
         * Synchronous variant of {@link StorageNamespace.rm}.
         */
        rmSync(path: string, options?: StorageRmOptions): void;
        /**
         * Removes one file path.
         */
        unlink(path: string): Promise<void>;
        /**
         * Synchronous variant of {@link StorageNamespace.unlink}.
         */
        unlinkSync(path: string): void;
        /**
         * Renames or moves a file path.
         */
        rename(oldPath: string, newPath: string, options?: StorageRenameOptions): Promise<void>;
        /**
         * Synchronous variant of {@link StorageNamespace.rename}.
         */
        renameSync(oldPath: string, newPath: string, options?: StorageRenameOptions): void;
        /**
         * Copies one file to another path.
         */
        copyFile(src: string, dest: string): Promise<void>;
        /**
         * Synchronous variant of {@link StorageNamespace.copyFile}.
         */
        copyFileSync(src: string, dest: string): void;
    }

    /**
     * Result of one KV list page.
     */
    interface KVListResult {
        keys: string[];
        items: { key: string; value: string }[];
        cursor: string;
        hasNext: boolean;
    }

    /**
     * Simple string KV operations scoped by namespace.
     */
    interface KVOps {
        get(key: string): string | null;
        set(key: string, value: string, ttl?: number): void;
        delete(key: string): boolean;
        putIfNotExists(key: string, value: string, ttl?: number): boolean;
        compareAndSwap(key: string, oldValue: string, newValue: string): boolean;
        list(limit?: number, cursor?: string): KVListResult;
    }

    /**
     * Access to org-scoped and repo-scoped KV namespaces.
     */
    interface KVSystem {
        repo(...group: string[]): KVOps;
        org(...group: string[]): KVOps;
    }

    /**
     * Broadcast event system backed by the server event bus.
     */
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

    /**
     * Host metadata exposed to page scripts.
     */
    interface PageHost {
        meta: PageMeta;
        auth: PageAuth;
    }

    /**
     * Current page metadata and request auth state.
     */
    const page: PageHost;
    /**
     * Read-only page source files from the current commit.
     */
    const fs: PageFS;
    /**
     * Read-write repo-scoped storage isolated to the current repo.
     */
    const storage: StorageNamespace;
    /**
     * String KV stores scoped to the current org or repo.
     */
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

    /**
     * Server-side WebSocket object returned by {@link upgradeWebSocket}.
     */
    interface PageWebSocket extends WebSocket {
        /**
         * Sends text or binary data. Typed-array views are sent using their
         * actual byte window, not the entire backing ArrayBuffer.
         */
        send(data: string | Uint8Array | ArrayBuffer): Promise<void>;
    }

    /**
     * Upgrades the current request to a WebSocket response pair.
     */
    function upgradeWebSocket(request?: Request): {
        socket: PageWebSocket;
        response: Response;
    };

    /**
     * Server-sent event writer returned by {@link http.sse}.
     */
    interface EventStream {
        /**
         * Encodes one SSE event and writes it to the stream.
         */
        send(data: string, options?: {
            event?: string;
            id?: string;
            retry?: number;
        }): Promise<void>;
        /**
         * Closes the SSE stream.
         */
        close(): void;
        /**
         * Whether the SSE stream has been closed.
         */
        readonly closed: boolean;
    }
}

export {};
