/* eslint-disable @typescript-eslint/no-explicit-any */
import { defu } from 'defu'
import type { RequestInterceptor, ResponseInterceptor, ErrorInterceptor, CustomFetchOptions } from "../types"

// Internal-only flags layered onto CustomFetchOptions so a request can be
// marked "already went through the refresh-retry cycle" and "this IS the
// refresh call itself" — keeps both out of the shared types file since
// nothing outside this class needs to set them.
type InternalFetchOptions<T = any> = CustomFetchOptions<T> & {
  _retriedAfterRefresh?: boolean
  _isAuthRefreshCall?: boolean
}

class ApiClient {
  private baseURL: string
  private defaultHeaders: HeadersInit
  private requestInterceptors: RequestInterceptor[] = []
  private responseInterceptors: ResponseInterceptor<any>[] = []
  private errorInterceptors: ErrorInterceptor[] = []

  // Route(s) that should never themselves trigger a refresh-on-401 — refreshing
  // in response to the refresh endpoint's own 401 would recurse forever.
  private authRefreshUrl = '/api/auth/refresh'
  private authLoginUrl = '/api/auth/login'

  // Dedupes concurrent refreshes: every 401 that lands while a refresh is
  // already in flight awaits this same promise instead of firing its own
  // POST /api/auth/refresh.
  private refreshPromise: Promise<void> | null = null

  // Optional hook for "the refresh itself failed" — the session is genuinely
  // over (expired/invalid refresh cookie), not just a request-level error.
  // Wire this to your auth store / router in app startup, e.g.:
  //   useApi().onSessionExpired(() => { authStore.clear(); navigateTo('/login') })
  private sessionExpiredHandler: (() => void) | null = null

  constructor() {
    const { public: { apiBase } } = useRuntimeConfig()
    this.baseURL = apiBase || import.meta.env.VITE_API_URL
    this.defaultHeaders = {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    }
  }

  // Add request interceptor
  addRequestInterceptor(interceptor: RequestInterceptor) {
    this.requestInterceptors.push(interceptor)
  }

  // Add response interceptor
  addResponseInterceptor<T>(interceptor: ResponseInterceptor<T>) {
    this.responseInterceptors.push(interceptor)
  }

  // Add error interceptor
  addErrorInterceptor(interceptor: ErrorInterceptor) {
    this.errorInterceptors.push(interceptor)
  }

  // Called when a refresh attempt itself fails — the refresh token is dead,
  // not just the access token expired. Typical usage: clear auth state and
  // redirect to login.
  onSessionExpired(handler: () => void) {
    this.sessionExpiredHandler = handler
  }

  private buildHeaders(options?: CustomFetchOptions): HeadersInit {
    const headers: HeadersInit = { ...this.defaultHeaders }

    if (options?.headers) {
      Object.assign(headers, options.headers)
    }

    return headers
  }

  private async handleError(error: any): Promise<never> {
    let processedError = {
      statusCode: error?.response?.status || 500,
      statusMessage: error?.response?.statusText || 'Internal Server Error',
      message: error?.data?.message || error?.message || 'An error occurred',
      data: error?.data
    }

    // Run error interceptors
    for (const interceptor of this.errorInterceptors) {
      try {
        await interceptor(processedError)
      } catch (err) {
        processedError = err as any
      }
    }

    if (import.meta.dev) {
      console.error('API Error:', processedError)
    }

    throw processedError
  }

  /**
   * Auth is cookie-based (httpOnly access_token/refresh_token, credentials:
   * "include"), so "refreshing" just means calling the refresh endpoint —
   * it rotates the cookies via Set-Cookie, and the retried request picks
   * them up automatically. Uses raw $fetch, never fetchWithRetry, so a
   * 401 on the refresh call itself can't recurse back into this method.
   */
  private refreshAccessToken(): Promise<void> {
    if (this.refreshPromise) {
      return this.refreshPromise
    }

    this.refreshPromise = $fetch(this.authRefreshUrl, {
      baseURL: this.baseURL,
      method: 'POST',
      credentials: 'include',
      headers: this.defaultHeaders,
    })
      .then(() => undefined)
      .catch((err) => {
        this.sessionExpiredHandler?.()
        throw err
      })
      .finally(() => {
        this.refreshPromise = null
      })

    return this.refreshPromise
  }

  private async fetchWithRetry<T>(
    url: string,
    options: InternalFetchOptions<T> = {}
  ): Promise<T> {
    const { retry = 0, retryDelay = 1000, timeout, _retriedAfterRefresh, _isAuthRefreshCall, ...fetchOptions } = options

    let processedUrl = url
    let processedOptions = fetchOptions

    // Run request interceptors
    for (const interceptor of this.requestInterceptors) {
      const result = await interceptor(processedUrl, processedOptions)
      if (result) {
        processedUrl = result.url || processedUrl
        processedOptions = result.options || processedOptions
      }
    }

    const headers = this.buildHeaders(options)

    const mergedOptions = defu(processedOptions, {
      baseURL: this.baseURL,
      headers,
      credentials: "include",
      ...(timeout && {
        signal: AbortSignal.timeout(timeout)
      })
    })

    let lastError: any

    for (let attempt = 0; attempt <= retry; attempt++) {
      try {
        let response = await $fetch<T>(processedUrl, mergedOptions)

        // Run response interceptors
        for (const interceptor of this.responseInterceptors) {
          response = await interceptor(response)
        }

        return response
      } catch (error: any) {
        lastError = error

        const status = error?.response?.status
        const isUnauthorized = status === 401
        const isAuthRoute = processedUrl === this.authRefreshUrl || processedUrl === this.authLoginUrl

        // 401, not from the auth routes themselves, and not already retried
        // once after a refresh → refresh, then retry this exact request one
        // time. Any 401 that happens AFTER that retry falls through to the
        // normal error path instead of looping forever.
        if (isUnauthorized && !isAuthRoute && !_retriedAfterRefresh) {
          try {
            await this.refreshAccessToken()
            return await this.fetchWithRetry<T>(url, {
              ...options,
              _retriedAfterRefresh: true,
            })
          } catch (refreshError) {
            return this.handleError(refreshError)
          }
        }

        if (status >= 400 && status < 500) {
          break
        }

        if (attempt < retry) {
          await new Promise(resolve => setTimeout(resolve, retryDelay * (attempt + 1)))
        }
      }
    }

    return this.handleError(lastError)
  }

  async get<T = any>(url: string, options?: CustomFetchOptions<T>): Promise<T> {
    return this.fetchWithRetry<T>(url, { ...options, method: 'GET' })
  }

  async post<T = any>(url: string, body?: any, options?: CustomFetchOptions<T>): Promise<T> {
    return this.fetchWithRetry<T>(url, { ...options, method: 'POST', body })
  }

  async put<T = any>(url: string, body?: any, options?: CustomFetchOptions<T>): Promise<T> {
    return this.fetchWithRetry<T>(url, { ...options, method: 'PUT', body })
  }

  async patch<T = any>(url: string, body?: any, options?: CustomFetchOptions<T>): Promise<T> {
    return this.fetchWithRetry<T>(url, { ...options, method: 'PATCH', body })
  }

  async delete<T = any>(url: string, options?: CustomFetchOptions<T>): Promise<T> {
    return this.fetchWithRetry<T>(url, { ...options, method: 'DELETE' })
  }

  async upload<T = any>(url: string, file: File | FormData, options?: CustomFetchOptions<T>): Promise<T> {
    const formData = file instanceof FormData ? file : new FormData()

    if (file instanceof File) {
      formData.append('file', file)
    }

    const headers = this.buildHeaders(options)
    delete (headers as any)['Content-Type']

    return this.fetchWithRetry<T>(url, {
      ...options,
      method: 'POST',
      body: formData,
      headers
    })
  }

  setHeader(key: string, value: string) {
    this.defaultHeaders = { ...this.defaultHeaders, [key]: value }
  }

  removeHeader(key: string) {
    const headers = { ...this.defaultHeaders }
    delete (headers as any)[key]
    this.defaultHeaders = headers
  }

  setBaseURL(url: string) {
    this.baseURL = url
  }
}

let apiClient: ApiClient | null = null

export const useApi = () => {
  if (!apiClient) {
    apiClient = new ApiClient()
  }
  return apiClient
}