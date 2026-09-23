// server/api/auth/me.get.ts

export default defineEventHandler(async (event) => {
  const token = getCookie(event, 'auth_token')

  if (!token) {
    throw createError({
      statusCode: 401,
      statusMessage: 'Unauthenticated',
    })
  }

  // Validate token / call your backend
  // const user = await getUserFromToken(token)

  return true //user
})
