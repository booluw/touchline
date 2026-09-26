<script lang="ts" setup>
import { useManagerOffer } from '~/composables/manager/offer';
import type { NextFixture, Offer } from '~/types';
import { formatMoneyCompact, formatFixtureDateTimeSmart } from '../../utils/helpers';
import { useManagerDashboard } from '~/composables/manager/dashboard';

const { getOffers } = useManagerOffer()
const { getDashboardData, getBoardStatus } = useManagerDashboard()
const { getFinance, getCompetitions, getFixtures, getNextFixture, getDynamics } = useClub()

const financeStore = useFinanceStore()
const clubStore = useClubStore()

const club = {
  competitions: computed(() => clubStore.competitions).value,
  club: computed(() => clubStore.club).value,
  game: ref<NextFixture>().value,
  dynamics: ref().value,
  board: computed(() => clubStore.board).value
}

const finance = {
  summary: computed(() => financeStore.summary)
}

const loading = reactive<Record<string, "loading" | "loaded" | "error">>({})
const offers = ref<Offer[]>([])
const offerToView = ref<Offer>()
const dashboard = ref()

async function getManagerOffers() {
  try {
    loading.offers = "loading"
    const { offers: response } = await getOffers()
    offers.value = response ?? []
    loading.offers = "loaded"
  } catch {
    loading.offers = "error"
  }
}

async function getManagerDashboardData() {
  try {
    loading.dashboard = "loading"
    dashboard.value = await getDashboardData()
    loading.dashboard = "loaded"
  } catch {
    loading.dashboard = "error"
  }
}

async function getClubFinance() {
  try {
    loading.finance = "loading"
    await getFinance()
    loading.finance = "loaded"
  } catch {
    loading.finance = "error"
  }
}

async function getClubCompetitions() {
  try {
    loading.competitions = "loading"
    await getCompetitions()
    loading.competitions = "loaded"
  } catch (error) {
    loading.competitions = "error"
  }
}

async function getClubNextFixtures() {
  try {
    loading.fixtures = "loading"
    club.game = await getNextFixture()
    loading.fixtures = "loaded"
  } catch (error) {
    loading.fixtures = "error"
  }
}

async function getClubDynamics() {
  try {
    loading.dynamics = "loading"
    club.dynamics = await getDynamics()
    loading.dynamics = "loaded"
  } catch (error) {
    loading.dynamics = "error"
  }
}

async function getBoardManagerStatus() {
  try {
    loading.board = "loading"
    await getBoardStatus()
    loading.board = "loaded"
  } catch (error) {
    loading.board = "error"
  }
}

async function init() {
  await Promise.all([
    getManagerDashboardData(),
    getManagerOffers(),
    getClubFinance(),
    getClubCompetitions(),
    getClubNextFixtures(),
    getClubDynamics(),
    getBoardManagerStatus(),
    // getClubFixtures()
  ])
}

onMounted(() => init())
</script>

<template>
  <section class="space-y-5 font-mono">
    <h2 class="page__header"></h2>
    <div class="grid gap-5 grid-cols-4 grid-rows-2">
      <div class="col-span-2 bg-void-900 border-brutal border-cyan-300 p-5 overflow-x-auto">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">Overview</h3>
        </div>
        <div class="mt-5 space-y-10">
          <div v-if="club.game" class="flex items-center gap-3">
            <!-- {{ club.game }} -->
            <div class="flex items-end">
              <h3 class="heading"
                :class="[club.game.fixture.status !== 'scheduled' ? 'text-sm text-void-300 w-15' : 'text-3xl']">{{ club.game.fixture.home_club.name }}</h3>
              <h2 v-if="club.game.fixture.status !== 'scheduled'" class="heading text-7xl">0</h2>
            </div>
            <div class="flex flex-col items-center">
              <span class="heading heading--big text-void-400">-</span>
              <div class="pill pill--live flex items-center gap-2">
                <div v-if="club.game.fixture.status === 'scheduled'" class="">
                  {{ formatFixtureDateTimeSmart(club.game.fixture.scheduled_at) }}
                </div>
                <template>
                  <div class="h-2 w-2 bg-live-500 rounded-full" />
                  78'
                </template>
              </div>
            </div>
            <div class="flex items-end gap-2">
              <h2 v-if="club.game.fixture.status !== 'scheduled'" class="heading text-7xl">0</h2>
              <h3 class="heading" :class="[club.game.fixture.status !== 'scheduled'? 'text-sm text-void-300 w-15' : 'text-3xl']">{{ club.game.fixture.away_club.name }}</h3>
            </div>
          </div>
          <div class="pt-5 border-t border-void-800 grid grid-cols-2 gap-5">
            <div class="">
              <h3 class="heading heading--small">board & fans</h3>

              <div class="">
                <div class="" v-if="loading.board === 'loading'">Please wait</div>
                <div class="mt-5 flex gap-5" v-else-if="loading.board === 'loaded'">
                  <div class="flex flex-col gap-1">
                    <div class="flex gap-2 items-center">
                      <h4 class="heading heading--medium">{{ club.board?.confidence }}</h4>
                      <IconsHappy class="w-10" v-if="club.board?.confidence! > 50" />
                      <IconsNeutral class="w-10" v-else-if="club.board?.confidence! === 50" />
                      <IconsSad class="w-10" v-else-if="club.board?.confidence! < 45" />
                      <IconsBlanked class="w-10" v-else />
                    </div>
                    <h5 class="heading heading--small">confidence</h5>
                  </div>

                  <div class="flex flex-col gap-1">
                    <div class="flex gap-2 items-center">
                      <h4 class="heading heading--medium">{{ club.board?.snapshot.scores.performance_score }}</h4>
                      <IconsHappy class="w-10" v-if="club.board?.snapshot.scores.performance_score! > 50" />
                      <IconsNeutral class="w-10" v-else-if="club.board?.snapshot.scores.performance_score! === 50" />
                      <IconsSad class="w-10" v-else-if="club.board?.snapshot.scores.performance_score! < 45" />
                      <IconsBlanked class="w-10" v-else />
                    </div>
                    <h5 class="heading heading--small">performance rating</h5>
                  </div>

                  <div class="flex flex-col gap-1">
                    <div class="flex gap-2 items-center">
                      <h4 class="heading heading--medium">{{ club.board?.snapshot.scores.supporter_sentiment_score }}
                      </h4>
                      <IconsHappy class="w-10" v-if="club.board?.snapshot.scores.supporter_sentiment_score! > 50" />
                      <IconsNeutral class="w-10"
                        v-else-if="club.board?.snapshot.scores.supporter_sentiment_score! === 50" />
                      <IconsSad class="w-10" v-else-if="club.board?.snapshot.scores.supporter_sentiment_score! < 45" />
                      <IconsBlanked class="w-10" v-else />
                    </div>
                    <h5 class="heading heading--small">fans score</h5>
                  </div>
                </div>
                <div v-else class="uppercase text-xs flex gap-3 mt-3">
                  <div class="text-loss-500">
                    An Error Occurred
                  </div>
                  <button class="uppercase text-cyan-300" @click="getClubFinance()">Retry</button>
                </div>
              </div>

            </div>
          </div>
          <!-- Current Match / Last Match Report / Next Match Report
          <br /><br />
          // cyan if there's a current match
          // normal for past or next match -->
        </div>
      </div>
      <div class="border-brutal border-void-500 p-5">
        <div v-if="loading.competitions !== 'loaded'"
          class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">Competitions</h3>
          <nuxt-link to="/play/competitions" class="font-mono text-cyan-500/50 hover:text-cyan-500 text-xs uppercase">
            all
          </nuxt-link>
        </div>

        <UiLoader v-if="loading.competitions === 'loading'" />
        <template v-else-if="loading.competitions === 'loaded'">
          <div class="carousel h-50 p-0 m-0">
            <div v-for="(competition, key) in club.competitions" :key class="carousel__item w-full h-full font-mono">
              <div class="flex items-center justify-between border-b-brutal pb-3 border-void-800">
                <h3 class="uppercase heading heading--small">
                  {{ competition[competition.competition_type as 'league']?.competition.name }}
                </h3>

                <nuxt-link to="/play/competitions"
                  class="font-mono text-cyan-500/50 hover:text-cyan-500 text-xs uppercase">
                  {{ competition.competition_type.split("_").join(" ") }}
                </nuxt-link>
              </div>
              <template v-if="competition[competition.competition_type as 'league']?.started">
                League Has started, show table or next fixture
              </template>
              <div v-else class="uppercase text-xs flex flex-col items-start gap-2 py-5">
                <div class="text-draw-500">
                  {{ competition.competition_type.split("_").join(" ") }} has not yet started
                </div>
              </div>
            </div>
          </div>
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="getClubCompetitions()">Retry</button>
        </div>
      </div>
      <div class="row-span-2 border-brutal border-void-800 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">News</h3>
        </div>

        <UiLoader v-if="loading.dashboard === 'loading'" />
        <div class="mt-5" v-else-if="loading.dashboard === 'loaded'">
          {{ dashboard }}
        </div>
      </div>
      <div class="border-brutal p-5 space-y-5"
        :class="[finance.summary.value?.cash <= 0 ? 'border-loss-500' : 'border-void-500']">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">Finance</h3>
        </div>

        <UiLoader v-if="loading.finance === 'loading'" />
        <template v-else-if="loading.finance === 'loaded'">
          <div class="grid gap-y-2 grid-cols-2 grid-rows-2">
            <div class="flex flex-col justify-center">
              <h3 class="heading heading--medium">
                {{ formatMoneyCompact(finance.summary.value!.cash) }}
              </h3>
              <h4 class="heading heading--small">available cash</h4>
            </div>
            <div class="">
              <h3 class="font-semibold text-2xl">{{
                formatMoneyCompact(finance.summary.value!.transfer_budget.available!) }}</h3>
              <h4 class="heading heading--small">transfer budget</h4>
            </div>
            <div class="">
              <h3 class="font-semibold text-2xl">{{
                formatMoneyCompact(finance.summary.value!.wage_budget.available!) }}</h3>
              <h4 class="heading heading--small">wage budget</h4>
            </div>
            <div class="">
              <h3 class="font-semibold text-2xl">{{
                formatMoneyCompact(finance.summary.value!.projected_year_end_balance) }}</h3>
              <h4 class="heading heading--small">projected year balance</h4>
            </div>
          </div>
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="getClubFinance()">Retry</button>
        </div>
      </div>
      <div class="col-span-2 row-span-2 border-brutal border-void-700 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">Squad</h3>
          <nuxt-link to="/play/squad" class="font-mono text-cyan-500/50 hover:text-cyan-500 text-xs uppercase">
            Manage
          </nuxt-link>
        </div>

        <div class="mt-5">
          // Red border if finance is in crisis
        </div>
      </div>
      <div class="border-brutal border-void-500 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">Facilities</h3>
        </div>

        <div class="mt-5">
          All Club facilities
        </div>
      </div>
      <div class="bg-void-900 border-brutal border-void-800 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">offers</h3>
        </div>

        <UiLoader v-if="loading.offers === 'loading'" />
        <template v-else-if="loading.offers === 'loaded'">
          <div v-if="offers.length === 0" class="p-10 uppercase text-xs text-void-400 text-center">
            No offers for you at this time.
          </div>
          <div v-else class="carousel">
            <div v-for="(offer, key) in offers" :key class="carousel__item bg-void-800 w-5/6 p-3">
              <div class="font-mono">
                <div class="flex items-center justify-between">
                  <h3 class="uppercase text-lg font-bold">{{ offer.club.name }}</h3>
                  <button class="heading heading--small cursor-pointer" @click="() => offerToView = offer">View</button>
                </div>
                <div class="mt-3">
                  <h4 class="heading heading--small">financials</h4>
                  <div class="flex justify-between">
                    <h4 class="">Transfer Budget:</h4>
                    <p class="font-semibold">{{ formatMoneyCompact(offer.finance.transfer_budget.available) }}</p>
                  </div>
                  <div class="flex justify-between">
                    <h4 class="">Wage Budget:</h4>
                    <p class="font-semibold">{{ formatMoneyCompact(offer.finance.wage_budget.available) }}</p>
                  </div>
                </div>
                <div class="mt-3 pt-3 border-t border-void-600">
                  <h4 class="heading heading--small">board</h4>
                  <div class="flex justify-between">
                    <h4 class="">Persona:</h4>
                    <p class="font-semibold capitalize">{{ offer.board.persona.split("_").join(" ") }}</p>
                  </div>
                </div>
                <div class="mt-3 pt-3 border-t border-void-600">
                  <h4 class="heading heading--small">competition</h4>
                  <div class="flex justify-between">
                    <nuxt-link :to="`/play/league/${offer.league.id}`" class="text-cyan-700">{{ offer.league.name
                      }}:</nuxt-link>
                    <p class="font-semibold capitalize">{{ offer.league.position ?? "-" }}</p>
                  </div>
                  <div class="flex justify-between">
                    <h4 class="">Form:</h4>
                    <p class="font-semibold capitalize">
                      P{{ offer.league.played }}
                      W{{ offer.league.won }}
                      D{{ offer.league.drawn }}
                      L{{ offer.league.lost }}
                    </p>
                  </div>
                </div>
              </div>
            </div>

            <OfferView v-if="offerToView" :offer="offerToView" @close="offerToView = undefined"
              @done="(e: boolean) => e ? init() : getManagerOffers()" />
          </div>
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="getManagerOffers()">Retry</button>
        </div>
      </div>
    </div>
  </section>
</template>