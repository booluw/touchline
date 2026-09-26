<script lang="ts" setup>
import { useAdminLeague } from '~/composables/admin/league';

const props = defineProps<{
  world_id: string,
  id: string,
  name: string,
  started?: boolean
}>()
const emits = defineEmits(["close", "done"])

const { setLeagueMatchDays, setLeagueStartDate } = useAdminLeague({ leagueId: props.id })

const daysOfTheWeek = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"]
const selectedDays = ref<number[]>([])
const startDate = ref()

function toggleSelectedDay(key: number) {
  if (selectedDays.value.includes(key)) {
    selectedDays.value.splice(selectedDays.value.indexOf(key), 1)
  } else {
    selectedDays.value.push(key)
  }
}

async function setDays() {
  if (selectedDays.value.length === 0) return
  try {
    await setLeagueMatchDays(selectedDays.value)
    emits("done")
  } catch (error) {
    console.error(error)
  }
}

async function setStartDay() {
  if (!startDate.value.trim()) return
  try {
    await setLeagueStartDate({ kickoff_date: startDate.value, worldId: props.world_id })
  } catch (error) {
    console.error(error)
  }
}
</script>

<template>
  <UiModal size="w-1/4 h-[500px]" @close="emits('close')">
    <div class="h-80 overflow-auto space-y-10">
      <div class="flex flex-col gap-3 pb-5 border-b-brutal">
        <h2 class="heading heading--small">Matchdays</h2>
        <div class="flex gap-2 flex-wrap mt-3">
          <button
            v-for="(day, key) in daysOfTheWeek"
            :key
            class="pill"
            :class="{ '[--bg:var(--color-cyan-500)] [--variant:var(--color-void-500)]' : selectedDays.includes(key) }"
            @click="toggleSelectedDay(key)"
          >
            {{ day }}
          </button>
        </div>
        <button class="button button--outline" @click="setDays()">Save League Days</button>
      </div>

      <div class="flex flex-col gap-3 pb-5 border-b-brutal">
        <h2 class="heading heading--small">Matchdays</h2>
        <div class="flex gap-2 flex-wrap mt-3">
          <UiInput type="date" placeholder="Start Date" v-model="startDate" />
        </div>
        {{ startDate }}
        <button class="button button--outline" @click="setStartDay()">Save League Start Date</button>
      </div>
    </div>
  </UiModal>
</template>