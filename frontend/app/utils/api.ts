/* eslint-disable @typescript-eslint/no-explicit-any */
import { defu } from 'defu'
import type { RequestInterceptor, ResponseInterceptor, ErrorInterceptor, CustomFetchOptions } from "../types"

class ApiClient {
  private baseURL: string
  private defaultHeaders: HeadersInit
  private requestInterceptors: RequestInterceptor[] = []
  private responseInterceptors: ResponseInterceptor<any>[] = []
  private errorInterceptors: ErrorInterceptor[] = []

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

  // private getAuthToken(): string | null {
  //   const authStore = useAuthStore()

  //   // if (import.meta.server) {
  //   //   const authCookie = useCookie('auth_token')
  //   //   return authCookie.value || null
  //   // } else {
  //   //   const authCookie = useCookie('auth_token')
  //   //   return authCookie.value || localStorage.getItem('auth_token')
  //   // }

  //   return authStore.auth!.token || ""
  // }

  private buildHeaders(options?: CustomFetchOptions): HeadersInit {
    const headers: HeadersInit = { ...this.defaultHeaders }

    if (options?.headers) {
      Object.assign(headers, options.headers)
    }

    // if (options?.auth !== false) {
    //   const token = this.getAuthToken()
    //   if (token) {
    //     Object.assign(headers, {
    //       'Authorization': `Bearer ${token}`
    //     })
    //   }
    // }

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

  private async fetchWithRetry<T>(
    url: string,
    options: CustomFetchOptions<T> = {}
  ): Promise<T> {
    const { retry = 0, retryDelay = 1000, timeout, ...fetchOptions } = options

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

        if (error?.response?.status >= 400 && error?.response?.status < 500) {
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