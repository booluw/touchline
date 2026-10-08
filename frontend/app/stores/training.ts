export interface TrainingPlan { club_id: string; archetype: string; effective_from_tick: number; last_applied_week?: number }
export const useTrainingStore = defineStore('training', {
  state: () => ({ value: null as TrainingPlan | null, saving: false }),
  actions: {
    async load(clubId: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { $api } = useNuxtApp()

      this.value = await $api.get(`${apiBase}/api/clubs/${clubId}/training-plan`);
    },
    async save(clubId: string, archetype: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { $api } = useNuxtApp()

      this.saving = true; try {
        await $api.post(`${apiBase}/api/clubs/${clubId}/training-plan`, { archetype })
        await this.load(clubId)
      } finally { this.saving = false }
    },
  },
})
