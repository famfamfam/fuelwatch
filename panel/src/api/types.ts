// Типы API панели — по docs/07-api.md.

export type Mode = "setup" | "armed" | "paused";

export interface User {
  id: number;
  login: string;
}

export interface DeviceSummary {
  id: string;
  device_id: string;
  name: string;
  mode: Mode;
  online: boolean;
  paired: boolean;
  disabled: boolean;
  last_seen_at: string | null;
  live_until: string | null;
  battery: number | null;
  charging: boolean | null;
  battery_temp_c: number | null;
  thermal_status: number | null;
  network: string | null;
  last_frame_id: string | null;
  last_frame_at: string | null;
  last_frame_age_s: number | null;
  last_error: string | null;
  config_version_server: number;
  config_version_device: number;
  open_issues: string[];
  visit: { id: number; started_at: string; last_seen_at: string } | null;
  app_version: string;
  model: string;
  android: string;
}

export interface Issue {
  id: number;
  device_id: string;
  type: string;
  opened_at: string;
  closed_at: string | null;
  details: unknown;
}

export interface CameraCaps {
  camera_id: string;
  zoom_min: number;
  zoom_max: number;
  resolutions: string[];
  manual_focus: boolean;
  fps_ranges: number[][];
  sensor_orientation: number;
}

export interface Heartbeat {
  uptime_s?: number;
  tx_bytes_today?: number | null;
  queue_items?: number;
  queue_mb?: number;
  [k: string]: unknown;
}

export interface DeviceDetail {
  device: DeviceSummary;
  last_heartbeat: Heartbeat | null;
  camera_caps: CameraCaps | null;
  issues: Issue[];
  reference_frame_id: string | null;
  zones_version: number;
  created_at: string;
}

export type CommandType = "snapshot" | "capture_reference" | "restart_camera" | "restart_app" | "arm" | "pause" | "live";
export type CommandStatus = "pending" | "sent" | "done" | "failed" | "expired";

export interface Command {
  id: string;
  device_id: string;
  type: CommandType;
  params: unknown;
  created_by: string | null;
  created_at: string;
  expires_at: string;
  sent_at: string | null;
  done_at: string | null;
  ok: boolean | null;
  error: string | null;
  status: CommandStatus;
}

export type Severity = "info" | "warning" | "critical" | "ok";

export interface Notification {
  id: number;
  device_id: string | null;
  type: string;
  severity: Severity;
  title: string;
  body: string;
  frame_id: string | null;
  visit_id: number | null;
  issue_id: number | null;
  created_at: string;
  read_at: string | null;
  telegram_status: "skipped" | "sent" | "failed";
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface PairingCode {
  pairing_code: string;
  expires_at: string;
}

// --- Шаг 2: настройки, зоны, кадры -----------------------------------------------------

export type SettingType = "int" | "float" | "bool" | "string" | "enum" | "string_list" | "enum_list";
export type SettingValue = number | boolean | string | string[];
export type SettingSource = "default" | "global" | "device";

export interface SettingDef {
  key: string;
  type: SettingType;
  default: SettingValue;
  min?: number;
  max?: number;
  step?: number;
  unit?: string;
  options?: string[];
  group: string;
  label: string;
  help?: string;
  scope: "device" | "server";
  overridable: boolean;
  camera_restart?: boolean;
  new_reference?: boolean;
  caps?: "zoom" | "resolutions" | "manual_focus";
}

export interface SettingsSchema {
  groups: { id: string; label: string }[];
  settings: SettingDef[];
}

export type DeviceSettings = Record<string, { value: SettingValue; source: SettingSource }>;

export type ZoneType = "MONITOR" | "IGNORE";
export type Point = [number, number];

export interface Zone {
  type: ZoneType;
  points: Point[];
}

export interface ZonesState {
  zones: Zone[] | null;
  version: number;
  reference_frame_id: string | null;
  config_version: number;
}

export interface ZoneVersion {
  version: number;
  zones: Zone[];
  reference_frame_id: string | null;
  created_by: string | null;
  created_at: string;
}

export type FrameKind = "keyframe" | "change" | "snapshot" | "reference";

/** Последний ответ модели по кадру (для бейджей). */
export interface ObservationBrief {
  tanker_present: boolean | null;
  tanker_in_zone: boolean | null;
  confidence: number | null;
  note: string | null;
  error: string | null;
  model: string;
}

export interface Frame {
  id: string;
  device_id: string;
  kind: FrameKind;
  taken_at: string;
  received_at: string;
  width: number;
  height: number;
  crop_rect: [number, number, number, number] | null;
  zoom: number | null;
  config_version: number | null;
  diff: number | null;
  command_id: string | null;
  status: "pending" | "processing" | "done" | "failed" | "skipped";
  observation: ObservationBrief | null;
}

export interface ObservationResult {
  tanker_present: boolean;
  confidence: number;
  tanker_in_zone: boolean;
  view_matches_reference: boolean;
  view_obstructed: boolean;
  note: string;
}

export interface Observation {
  id: number;
  result: ObservationResult | null;
  provider: string;
  model: string;
  prompt_version: string;
  tokens_in: number;
  tokens_out: number;
  cost: number;
  latency_ms: number;
  error: string | null;
  dry_run: boolean;
  created_at: string;
}

export interface Interval {
  from: string;
  to: string | null;
}

export interface Timeline {
  frames: { id: string; taken_at: string; kind: FrameKind }[];
  offline: Interval[];
  visits: Interval[];
}

export interface Visit {
  id: number;
  device_id: string;
  device_name: string;
  state: "PRESENT" | "LEFT";
  started_at: string;
  confirmed_at: string;
  last_seen_at: string;
  ended_at: string | null;
  end_reason: "left" | "max_hours" | "manual" | null;
  first_frame_id: string | null;
  last_frame_id: string | null;
  feedback: "up" | "down" | null;
}

export interface CostBucket {
  day?: string;
  model?: string;
  device_id?: string;
  calls: number;
  errors: number;
  tokens_in: number;
  tokens_out: number;
  cost: number;
}
