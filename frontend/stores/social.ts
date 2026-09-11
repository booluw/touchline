// Pinia store: social.ts — messaging, relationships, promises, manager profiles
export const useSocialStore = defineStore('social', {
  state: () => ({
    messages: [] as Record<string, unknown>[],
    relationships: [] as Record<string, unknown>[],
  }),

  actions: {
    async fetchMessages() {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/messages`)
      this.messages = data.value as Record<string, unknown>[] ?? []
    },
    async sendMessage(receiverId: string, content: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/messages`, {
        method: 'POST',
        body: { receiverId, content },
      })
      return data.value
    },
  },
})