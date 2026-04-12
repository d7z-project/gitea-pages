declare global {
    interface Blob {
        bytes(): Promise<Uint8Array>;
    }

    interface Request {
        bytes(): Promise<Uint8Array>;
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
        read(path: string): Promise<Uint8Array>;
        readText(path: string): Promise<string>;
        readSync(path: string): Uint8Array;
        readTextSync(path: string): string;
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
    // @ts-ignore
    const event: EventSystem;

    interface PageWebSocket extends WebSocket {
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
    }
}

export {};
