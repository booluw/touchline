<script lang="ts" setup>
import z from 'zod';
import type { World, Country } from '~/types';

const route = useRoute()
const router = useRouter()
const store = useAdminStore()
const { createLeague } = useAdmin()

const worldId = computed(() => route.params.id)
const countryId = computed(() => route.params.countryId)

const country = computed(() => store.countries?.find((c: Country) => c.id === countryId.value))
const leagues = computed(() => store.leagues?.filter((l: League) => l.country_id === countryId.value) ?? [])

const schema = z.object({
  name: z.string().min(3, 'league name must be more than 3 letters'),
  team_count: z.number().min(5, 'league requires at least 5 teams'),
  promotes_to: z.string().nullable(),
  promotions: z.number(),
  relegates_to: z.string().nullable(),
  relegations: z.number(),
  tier: z.number(),
})
const loading = ref(false)
const state = ref({
  name: '',
  team_count: 0,
  promotes_to: null,
  promotions: 0,
  relegates_to: null,
  relegations: 0,
  tier: 1,
  country_id: countryId.value
})

async function createNewLeague(valid: boolean, errors: Record<string, string>) {
  console.log(valid, errors)
  if (valid) {
    loading.value = true
    try {
      await createLeague(state.value)
      router.go(-1)
    } finally {
      loading.value = false
    }
  }
}
</script>

<template>
  <UiSlide @close="() => router.go(-1)" title="Create League" :description="`Create a new league for ${country.name}`">
    <UiForm @submit="createNewLeague" class="h-full flex flex-col gap-10" :state :schema>
      <div class="">
        <div class="grid gap-3 md:grid-cols-5">
          <UiFormItem class="col-span-3" label="Name" prop="name">
            <UiInput v-model="state.name" :placeholder="`The ${country.name} Premier League`" />
          </UiFormItem>
          <UiFormItem label="Tier" prop="tier">
            <UiInput v-model="state.tier" type="number" placeholder="League Tier" />
          </UiFormItem>
          <UiFormItem label="Teams" prop="team_count">
            <UiInput v-model="state.team_count" type="number" placeholder="Number of teams" />
          </UiFormItem>
        </div>

        <div class="">
          <h3 class="mb-2 font-mono uppercase text-sm font-semibold">Promotions</h3>
          <div class="grid md:grid-cols-2 gap-3">
            <UiFormItem label="Promotion league" prop="promotes_to">
              <UiSelect v-model="state.promotes_to" :options="leagues" item-id="id" item-val="name" placeholder="Select League For Promotion" />
            </UiFormItem>
            <UiFormItem label="Teams To Promote" prop="promotions">
              <UiInput v-model="state.promotions" type="number" placeholder="Number of teams to promote" />
            </UiFormItem>
          </div>
        </div>

        <div class="">
          <h3 class="mb-2 font-mono uppercase text-sm font-semibold">Relegations</h3>
          <div class="grid md:grid-cols-2 gap-3">
            <UiFormItem label="Relegation league" prop="relegates_to">
              <UiSelect v-model="state.relegates_to" :options="leagues" item-id="id" item-val="name"
                placeholder="Select League For Relegation" />
            </UiFormItem>
            <UiFormItem label="Teams To Relegate" prop="relegations">
              <UiInput v-model="state.relegations" type="number" placeholder="Number of teams to relegate" />
            </UiFormItem>
          </div>
        </div>
      </div>

      <button type="submit" class="button button--primary">
        Create {{ country.name }} League
      </button>
    </UiForm>
  </UiSlide>
</template>