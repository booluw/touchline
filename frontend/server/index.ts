// BFF server routes — auth cookie handling, SSR-safe proxying only.
// NOT game logic. Game logic lives in the Go backend.
export default defineEventHandler(async (event) => {
  return { status: 'ok', service: 'touchline-frontend-bff' }
})