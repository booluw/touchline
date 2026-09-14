export interface TacticsView { club_id: string; style: string; formation: string; allowed_formations: string[] }
export const useTacticsStore = defineStore('tactics', {
  state: () => ({ value: null as TacticsView | null, error: '', saving: false }),
  actions: {
    async load(clubId: string) { const { authedFetch } = useAuth(); const r = await authedFetch(`/api/clubs/${clubId}/tactics`); if (!r.ok) throw new Error('Could not load tactics.'); this.value = await r.json() },
    async save(clubId: string, style: string, formation: string) { const { authedFetch } = useAuth(); this.saving = true; try { const r = await authedFetch(`/api/clubs/${clubId}/tactics`, { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({style, formation}) }); if (!r.ok) throw new Error((await r.json()).error ?? 'Could not save tactics.'); await this.load(clubId) } finally { this.saving = false } },
  },
})
