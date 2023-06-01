import type { OpenAPIV3 } from "openapi-types";
import type { FieldRow } from "./components/shared/FieldRows";

export type APIDoc = OpenAPIV3.Document;

export type Method =
  | "GET"
  | "PUT"
  | "POST"
  | "DELETE"
  | "OPTIONS"
  | "HEAD"
  | "PATCH"
  | "TRACE";

export interface WorkerResource {
  workerKey: string;
  topicKey: string;
}
export type Schedule = WorkerResource;

export type Subscription = WorkerResource;

export type Topic = Schedule;

export interface History {
  apis: ApiHistoryItem[];
  schedules: EventHistoryItem[];
  topics: EventHistoryItem[];
}

export interface StackData {
  topics?: WorkerResource[];
  schedules?: WorkerResource[];
  buckets?: string[];
  collections?: string[];
  queues?: string[];
  secrets?: string[];
  apis?: APIDoc[];
}

export interface WebSocketResponse extends StackData {
  projectName: string;
  triggerAddress: string;
  apiAddresses: Record<string, string>;
  storageAddress: string; // has http:// prefix
}

export interface Param {
  path: string;
  value: OpenAPIV3.ParameterObject[];
}

export interface Endpoint {
  id: string;
  api: string;
  path: string;
  method: Method;
  params?: Param[];
  doc: OpenAPIV3.Document<Record<string, any>>;
}

export interface APIRequest {
  path?: string;
  method?: Method;
  pathParams: FieldRow[] | [];
  queryParams: FieldRow[];
  headers: FieldRow[];
  body?: BodyInit | null;
}

export interface APIResponse {
  data?: any;
  status?: number;
  time?: number;
  size?: number;
  headers?: Record<string, string>;
}

export interface BucketFile {
  Key: string;
}

export interface TopicRequest {
  id: string;
  payloadType: string;
  payload: Record<string, string>;
}

// HISTORY //

/** Used only in local storage to store the last used params in a request */
export interface LocalStorageHistoryItem {
  request: APIRequest;
  JSONBody: string;
}

/** History that is received from the CLI web socket */
export interface HistoryItem {
  time: number;
  success: boolean;
}

export interface EventHistoryItem extends HistoryItem {
  event: WorkerResource;
  payload: string;
}

export interface ApiHistoryItem extends HistoryItem {
  api: string;
  request: RequestHistory;
  response: APIResponse;
}

export interface RequestHistory {
  path?: string;
  method?: Method;
  pathParams: FieldRow[] | [];
  queryParams: FieldRow[] | [];
  headers: Record<string, string[]>;
  body?: BodyInit | null;
}
