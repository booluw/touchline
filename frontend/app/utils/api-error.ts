/* eslint-disable @typescript-eslint/no-explicit-any */
import { ApiError } from "../types/api";
import type { ServerErrorBody } from "../types/api";

// Client-side copy for failures the backend didn't describe itself.
function httpFallbackMessage(status: number): string {
  if (status === 401) return "Your session has expired. Please sign in again.";
  if (status === 403) return "You don't have permission to do that.";
  if (status === 404) return "The requested resource was not found.";
  if (status === 429) return "Too many requests. Please wait a moment and try again.";
  if (status >= 500) return "The server is unavailable right now. Please try again shortly.";
  return "The request could not be completed.";
}

// Normalises an ofetch FetchError (or anything thrown mid-request) into an
// ApiError, deciding whether it came from the backend, bare HTTP, or the network.
export function toApiError(error: any): ApiError {
  if (error instanceof ApiError) return error;

  const response = error?.response;
  if (!response) {
    const timedOut = error?.name === "TimeoutError" || error?.cause?.name === "TimeoutError";
    return new ApiError(
      "network",
      timedOut ? "timeout" : "network_error",
      0,
      timedOut ? "Timeout" : "Network Error",
      timedOut
        ? "The request timed out. Check your connection and try again."
        : "Could not reach the server. Check your connection and try again.",
    );
  }

  const status: number = response.status;
  const statusText: string = response.statusText || "Error";
  const data = error?.data;
  if (typeof data?.error === "string" && data.error) {
    return new ApiError("server", data.code || "error", status, statusText, data.error, data as ServerErrorBody);
  }
  return new ApiError("http", "http_error", status, statusText, httpFallbackMessage(status));
}
