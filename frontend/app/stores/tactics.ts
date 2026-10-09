export interface TacticsView { club_id: string; style: string; formation: string; allowed_formations: string[] }
export const useTacticsStore = defineStore('tactics', {
  state: () => ({ value: null as TacticsView | null, error: '', saving: false }),
  actions: {
    async load(clubId: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { $api } = useNuxtApp()

      const r = await $api.get(`${apiBase}/api/clubs/${clubId}/tactics`);
      if (!r.ok) throw new Error('Could not load tactics.');
      this.value = await r.json()
    },
    async save(clubId: string, style: string, formation: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { $api } = useNuxtApp()

      this.saving = true;
      try {
        await $api.post(`${apiBase}/api/clubs/${clubId}/tactics`, { style, formation });
        await this.load(clubId)
      } finally { this.saving = false }
    },
  },
})
