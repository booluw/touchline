export interface TrainingPlan { club_id: string; archetype: string; effective_from_tick: number; last_applied_week?: number }
export const useTrainingStore = defineStore('training', {
  state: () => ({ value: null as TrainingPlan | null, saving: false }),
  actions: {
    async load(clubId: string) { const { authedFetch } = useAuth(); const r = await authedFetch(`/api/clubs/${clubId}/training-plan`); if (!r.ok) throw new Error('Could not load training plan.'); this.value = await r.json() },
    async save(clubId: string, archetype: string) { const { authedFetch } = useAuth(); this.saving = true; try { const r = await authedFetch(`/api/clubs/${clubId}/training-plan`, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({archetype})}); if (!r.ok) throw new Error((await r.json()).error ?? 'Could not save plan.'); await this.load(clubId) } finally { this.saving = false } },
  },
})
