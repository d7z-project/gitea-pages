declare global {
    interface Headers {
        get(name: string): string | null;
        set(name: string, value: string): void;
        append(name: string, value: string): void;
        has(name: string): boolean;
        delete(name: string): void;
        keys(): string[];
        values(): string[];
        entries(): [string, string][];
        forEach(callback: (value: string, key: string, parent: Headers) => void): void;
    }

    interface RequestInit {
        method?: string;
        headers?: Headers | Record<string, string>;
        body?: string | Uint8Array | ArrayBuffer;
        signal?: AbortSignal;
    }

    interface Request {
        readonly method: string;
        readonly url: string;
        readonly headers: Headers;
        readonly bodyUsed: boolean;
        readonly signal: AbortSignal;
        text(): Promise<string>;
        json<T = any>(): Promise<T>;
        arrayBuffer(): Promise<ArrayBuffer>;
        clone(): Request;
    }

    interface ResponseInit {
        status?: number;
        statusText?: string;
        headers?: Headers | Record<string, string>;
    }

    interface Response {
        readonly status: number;
        readonly statusText: string;
        readonly headers: Headers;
        readonly ok: boolean;
        readonly bodyUsed: boolean;
        text(): Promise<string>;
        json<T = any>(): Promise<T>;
        arrayBuffer(): Promise<ArrayBuffer>;
        clone(): Response;
    }

    var Request: {
        new(input: string | Request, init?: RequestInit): Request;
    };

    var Response: {
        new(body?: string | Uint8Array | ArrayBuffer | null, init?: ResponseInit): Response;
        json(data: any, init?: ResponseInit): Response;
        redirect(location: string, status?: number): Response;
    };

    interface FetchOptions extends RequestInit {}

    function fetch(url: string, options?: FetchOptions): Promise<Response>;

    type RequestHandler = (request: Request) => Response | Promise<Response>;
    type FetchHandlerObject = {
        fetch(request: Request): Response | Promise<Response>;
    };
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
        cors(response: Response, options?: {
            origin?: string;
            methods?: string | string[];
            headers?: string | string[];
            exposeHeaders?: string | string[];
            credentials?: boolean;
        }): Promise<Response>;
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
        load(key: string): Promise<any>;
        put(key: string, value: string): Promise<void>;
    }

    interface PageHost {
        meta: PageMeta;
        auth: PageAuth;
    }

    const page: PageHost;
    const fs: PageFS;
    const kv: KVSystem;
    const event: EventSystem;
    function upgradeWebSocket(request?: Request): {
        socket: WebSocket;
        response: Response;
    };

    interface EventStream {
        send(data: string, options?: {
            event?: string;
            id?: string;
            retry?: number;
        }): Promise<void>;
        close(): void;
    }

    interface WebSocketEventMap {
        open: Event;
        message: MessageEvent<string | Uint8Array>;
        close: CloseEvent;
        error: Event;
    }

    interface Event {
        type: string;
    }

    interface MessageEvent<T = any> extends Event {
        data: T;
    }

    interface CloseEvent extends Event {
        code?: number;
        reason?: string;
        wasClean?: boolean;
    }

    interface WebSocket {
        readonly CONNECTING: 0;
        readonly OPEN: 1;
        readonly CLOSING: 2;
        readonly CLOSED: 3;
        readonly readyState: number;
        onopen: ((event: Event) => void) | null;
        onmessage: ((event: MessageEvent<string | Uint8Array>) => void) | null;
        onerror: ((event: Event) => void) | null;
        onclose: ((event: CloseEvent) => void) | null;
        addEventListener<K extends keyof WebSocketEventMap>(type: K, listener: (event: WebSocketEventMap[K]) => void): void;
        send(data: string | Uint8Array | ArrayBuffer): Promise<void>;
        close(code?: number): void;
    }

    interface Console {
        log(...args: any[]): void;
        warn(...args: any[]): void;
        error(...args: any[]): void;
        info(...args: any[]): void;
        debug(...args: any[]): void;
    }

    const console: Console;
}

export {};
