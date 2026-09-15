/* eslint-disable @typescript-eslint/no-explicit-any */
import type { UseFetchOptions } from 'nuxt/app'

export interface ApiResponse<T = any> {
  data?: T
  error?: string
  message?: string
  success: boolean
}

export interface ApiError {
  statusCode: number
  statusMessage: string
  message: string
  data?: any
}

export interface CustomFetchOptions<T = any> extends UseFetchOptions<T> {
  auth?: boolean
  headers?: HeadersInit
  retry?: number
  retryDelay?: number
  timeout?: number
}

export type RequestInterceptor = (url: string, options: any) => Promise<any> | any
export type ResponseInterceptor<T> = (response: T) => Promise<T> | T
export type ErrorInterceptor = (error: any) => Promise<never> | never