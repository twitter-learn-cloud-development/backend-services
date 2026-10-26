export type Mode = "demo" | "live";
export type SupportStatus =
  | "answered"
  | "partial"
  | "clarification_required"
  | "unsupported";
export interface SupportRequest {
  message: string;
  client_timezone: string;
  conversation_id?: string;
}
export interface SupportResponse {
  request_id: string;
  conversation_id: string;
  case_id: string;
  status: SupportStatus;
  answer: string;
  suggested_actions: string[];
  evidence_refs: string[];
  citations?: SupportCitation[];
  created_at: string;
  trace_id: string;
}
export interface SupportCitation {
  id: string;
  kind: "tweet";
  label: string;
  path: string;
  evidence_ref: string;
}
export interface Attempt {
  key: string;
  body: SupportRequest;
  threadId: string;
}
export interface ChatMessage {
  id: string;
  role: "user" | "assistant" | "error";
  text: string;
  createdAt: string;
  response?: SupportResponse;
  status?: SupportStatus;
  actions?: string[];
  attempt?: Attempt;
  requestId?: string;
}
export interface Conversation {
  id: string;
  remoteId?: string;
  title: string;
  updatedAt: string;
  messages: ChatMessage[];
  sample?: boolean;
}
export interface Account {
  id: string;
  username: string;
  email: string;
}
export interface DemoReply {
  answer: string;
  status: SupportStatus;
  actions: string[];
}
