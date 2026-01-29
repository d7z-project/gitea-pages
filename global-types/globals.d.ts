// global.d.ts


declare global {
    // WebSocket 相关类型
    interface WebSocketConnection {
        TypeTextMessage: number;
        TypeBinaryMessage: number;

        readText(): Promise<string>;

        read(): Promise<{
            type: number;
            data: Uint8Array;
        }>;

        writeText(data: string): Promise<void>;

        write(mType: number, data: string | Uint8Array): Promise<void>;
    }

    function websocket(): WebSocketConnection;

    // Event 相关类型
    interface EventSystem {
        load(key: string): Promise<any>;

        put(key: string, value: string): Promise<void>;
    }

    // @ts-ignore
    const event: EventSystem;

    //元数据
    const meta:Meta;

    interface Meta {
        // 组织名称
        org:string;
        // 仓库名称
        repo:string;
        // 提交 id
        commit:string;
    }

    // Request 相关类型
    interface RequestObject {
        method: string;
        url: string;
        rawPath: string;
        host: string;
        remoteAddr: string;
        remoteIP: string;
        proto: string;
        httpVersion: string;
        path: string;
        query: Record<string, string>;
        headers: Record<string, string>;

        get(key: string): string | null;

        getQuery(key: string): string;

        getHeader(name: string): string;

        getHeaderNames(): string[];

        getHeaders(): Record<string, string>;

        getRawHeaderNames(): string[];

        hasHeader(name: string): boolean;

        readBody(): Uint8Array;

        protocol: string;
    }

    const request: RequestObject;

    // Response 相关类型
    interface CookieOptions {
        maxAge?: number;
        expires?: number;
        path?: string;
        domain?: string;
        secure?: boolean;
        httpOnly?: boolean;
        sameSite?: "lax" | "strict" | "none";
    }

    interface ResponseObject {
        setHeader(key: string, value: string): void;

        getHeader(key: string): string;

        removeHeader(key: string): void;

        hasHeader(key: string): boolean;

        setStatus(statusCode: number): void;

        statusCode(statusCode: number): void;

        write(data: string): void;

        writeHead(statusCode: number, headers?: Record<string, string>): void;

        end(data?: string): void;

        redirect(location: string, statusCode?: number): void;

        json(data: any): void;

        setCookie(name: string, value: string, options?: CookieOptions): void;
    }

    const response: ResponseObject;

    // KV 存储相关类型
    interface KVListResult {
        keys: string[];
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

    const kv: KVSystem;

    // Console 相关 (假设通过 require 引入)
    interface Console {
        log(...args: any[]): void;

        warn(...args: any[]): void;

        error(...args: any[]): void;

        info(...args: any[]): void;

        debug(...args: any[]): void;
    }

    // @ts-ignore
    const console: Console;

    // Fetch API 相关类型
    interface FetchResponse {
        ok: boolean;
        status: number;
        statusText: string;
        headers: Record<string, string>;
        text(): Promise<string>;
        json<T = any>(): Promise<T>;
    }

    interface FetchOptions {
        method?: string;
        headers?: Record<string, string>;
        body?: string;
    }

    function fetch(url: string, options?: FetchOptions): Promise<FetchResponse>;
}

export {};