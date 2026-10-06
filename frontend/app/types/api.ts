/* eslint-disable @typescript-eslint/no-explicit-any */
import type { UseFetchOptions } from "nuxt/app";

export interface ApiResponse<T = any> {
  data?: T;
  error?: string;
  message?: string;
  success: boolean;
}

// Codes emitted by backend/internal/httpapi (respondError literals and
// error_codes.go sentinelCodes), plus status-text fallbacks for unlisted
// domain errors. Snapshot — when adding a backend code, add it here too;
// `(string & {})` keeps unknown future codes assignable.
export type ServerErrorCode =
  | "adjacency_mismatch"
  | "bad_adjacency"
  | "bid_expired"
  | "bid_resolved"
  | "bot_recipient"
  | "cannot_start_a_campaign_for_this_competition_type"
  | "choice_not_eligible"
  | "club_already_in_league"
  | "club_cannot_afford"
  | "club_has_offer"
  | "club_id_and_manager_id_are_required"
  | "club_name_part_required"
  | "club_name_required"
  | "club_name_taken"
  | "club_not_found"
  | "club_not_found_in_world"
  | "club_not_in_league"
  | "club_not_playable"
  | "club_occupied"
  | "club_world_mismatch"
  | "competition_is_not_a_domestic_cup"
  | "competition_is_not_a_league"
  | "competition_not_found"
  | "competition_not_seeded"
  | "competition_type_mismatch"
  | "competition_world_mismatch"
  | "counter_limit_reached"
  | "country_id_name_and_team_count_are_required"
  | "country_id_or_region_id_is_required"
  | "country_not_found"
  | "country_not_found_in_world"
  | "cup_campaign_exists"
  | "cup_final_date_invalid"
  | "cup_final_date_locked"
  | "cup_id_is_required"
  | "cup_limit"
  | "cup_not_found"
  | "dashboard_unavailable"
  | "duplicate_listing"
  | "duplicate_open_bid"
  | "email_and_password_are_required"
  | "email_taken"
  | "empty_message"
  | "final_date_or_final_offset_days_is_required"
  | "fixture_live"
  | "fixture_not_found"
  | "forbidden"
  | "insufficient_funds"
  | "internal_server_error"
  | "invalid_action_for_role"
  | "invalid_age_max"
  | "invalid_age_min"
  | "invalid_archetype"
  | "invalid_away_payload"
  | "invalid_bid_id"
  | "invalid_bid_payload"
  | "invalid_body"
  | "invalid_club_id"
  | "invalid_club_name_country"
  | "invalid_club_name_part"
  | "invalid_competition_id"
  | "invalid_country_id"
  | "invalid_counts"
  | "invalid_cup_id"
  | "invalid_email_or_password"
  | "invalid_fixture_id"
  | "invalid_formation"
  | "invalid_kickoff_date_body"
  | "invalid_league_id"
  | "invalid_lineup"
  | "invalid_listing_id"
  | "invalid_listing_payload"
  | "invalid_manager_id"
  | "invalid_mandate_id"
  | "invalid_match_id"
  | "invalid_matchday"
  | "invalid_message_id"
  | "invalid_negotiation_payload"
  | "invalid_offer_id"
  | "invalid_or_expired_session"
  | "invalid_player_id"
  | "invalid_policy_params"
  | "invalid_policy_type"
  | "invalid_recipient_id"
  | "invalid_region_id"
  | "invalid_respond_payload"
  | "invalid_season"
  | "invalid_style"
  | "invalid_tactical_change"
  | "invalid_tactics"
  | "invalid_team_count"
  | "invalid_terms"
  | "invalid_tier"
  | "invalid_training_plan"
  | "invalid_transition"
  | "invalid_world_id"
  | "investment_tier_must_be_between_1_and_5"
  | "key_is_required"
  | "kickoff_date_in_past"
  | "kickoff_date_must_be_a_calendar_date_yyyy_mm_dd"
  | "kickoff_not_allowed_weekday"
  | "league_already_seeded"
  | "league_full"
  | "league_id_is_required"
  | "league_not_found"
  | "league_shrink"
  | "malformed_adjacency_payload"
  | "malformed_bulk_payload"
  | "malformed_capacity_payload"
  | "malformed_cup_payload"
  | "malformed_final_date_payload"
  | "malformed_league_payload"
  | "malformed_preview_payload"
  | "malformed_qualification_payload"
  | "manager_employed"
  | "manager_not_found"
  | "manager_unavailable"
  | "mandate_resolved"
  | "mandate_type_not_negotiable"
  | "mandate_value_invalid"
  | "match_not_found"
  | "match_not_live"
  | "message_exceeds_the_maximum_length"
  | "message_not_found"
  | "message_rate_limit_exceeded"
  | "minute_closed"
  | "missing_refresh_token"
  | "name_collision"
  | "name_is_required"
  | "negotiation_rejected"
  | "news_unavailable"
  | "no_active_club"
  | "no_active_contract"
  | "no_world_joined"
  | "no_open_injury"
  | "no_open_transfer_request"
  | "no_policy_saved"
  | "no_season"
  | "no_world_context"
  | "non_regional_cup_has_no_country"
  | "not_ai_club"
  | "not_employed"
  | "not_found"
  | "not_free_agent"
  | "not_member"
  | "not_offer_candidate"
  | "nothing_to_update"
  | "offer_not_found"
  | "offer_resolved"
  | "outside_your_world"
  | "password_too_long"
  | "player_id_is_required"
  | "player_is_a_free_agent"
  | "player_not_found"
  | "player_not_transferable"
  | "player_unavailable"
  | "qualification_field"
  | "qualification_overlap"
  | "qualification_unavailable"
  | "reason_is_required"
  | "recipient_id_and_body_are_required"
  | "recipient_not_found"
  | "region_id_is_required_for_a_regional_cup"
  | "region_mismatch"
  | "region_name_collision"
  | "region_name_is_required"
  | "region_not_found"
  | "region_world_mismatch"
  | "rename_news_required"
  | "reputation_is_required"
  | "reputation_out_of_range"
  | "request_already_resolved"
  | "request_body_must_be_json_with_email_and_password"
  | "request_body_must_be_json_with_email_password_and_optional_display_name"
  | "season_not_found"
  | "seed_job_enqueuer_not_wired"
  | "self_message"
  | "staging_invalid"
  | "status_is_required"
  | "street_under18"
  | "team_count_promotions_and_relegations_are_required"
  | "the_league_does_not_belong_to_this_country"
  | "unauthenticated"
  | "weekly_wage_and_years_must_be_positive"
  | "world_archived"
  | "world_has_no_leagues"
  | "world_id_and_name_are_required"
  | "world_id_and_region_id_are_required"
  | "world_id_code_and_name_are_required"
  | "world_id_is_required"
  | "world_id_query_parameter_is_required"
  | "world_mismatch"
  | "world_not_found"
  | "bad_request"
  | "unauthorized"
  | "conflict"
  | "unprocessable_entity"
  | "too_many_requests"
  | "service_unavailable"
  | (string & {});

// Client-side codes for failures the backend didn't describe itself.
export type ClientErrorCode = "network_error" | "timeout" | "http_error";

// Every backend error response: `{"error": "<message>", "code": "<code>"}`
// (backend/internal/httpapi/errors.go). Branch on `code`; `error` is display text.
export interface ServerErrorBody {
  error: string;
  code: ServerErrorCode;
}

// Where a failure came from:
//  - "server":  the backend answered with its own `{"error": ...}` body;
//               `message` is the server's text, safe to show as-is.
//  - "http":    an HTTP error status without a backend body (proxy/gateway
//               502/504, HTML error page, 404 from the host); `message` is
//               a client-side fallback chosen by status.
//  - "network": no HTTP response at all (offline, DNS, CORS, timeout).
export type ApiErrorKind = "server" | "http" | "network";

// Thrown by every useApi() call on failure. A real Error subclass so
// `e instanceof Error ? e.message : ...` in callers shows the server message.
// statusCode 0 = no HTTP response (network failure, timeout, CORS).
export class ApiError extends Error {
  constructor(
    public kind: ApiErrorKind,
    // Server's code when kind === "server", else a ClientErrorCode.
    public code: ServerErrorCode | ClientErrorCode,
    public statusCode: number,
    public statusMessage: string,
    message: string,
    public data?: ServerErrorBody,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export const isApiError = (e: unknown): e is ApiError => e instanceof ApiError;

export interface CustomFetchOptions<T = any> extends UseFetchOptions<T> {
  auth?: boolean;
  headers?: HeadersInit;
  retry?: number;
  retryDelay?: number;
  timeout?: number;
}

export type RequestInterceptor = (
  url: string,
  options: any,
) => Promise<any> | any;
export type ResponseInterceptor<T> = (response: T) => Promise<T> | T;
export type ErrorInterceptor = (error: ApiError) => Promise<never> | never;
