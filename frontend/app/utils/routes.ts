import {
  PhArrowsLeftRight, PhBarbell, PhBinoculars, PhBriefcase, PhCalendarBlank, PhChatsCircle, PhCoins, PhGlobeHemisphereWest,
  PhHouse, PhShieldStar, PhStrategy, PhStudent, PhTrophy, PhUsersThree,
} from '@phosphor-icons/vue'
import type { ShellNavItem } from '~/types/ui/design'

export const ADMIN_ROUTES = {
  links: [
    {
      to: '/admin/world',
      text: 'Worlds'
    },
    {
      to: '/admin/leagues',
      text: 'Leagues'
    },
    {
      to: '/admin/clubs',
      text: 'Clubs'
    },
    {
      to: '/admin/managers',
      text: 'managers'
    },
    {
      to: '/admin/players',
      text: 'Players'
    }
  ],
  cta: {
    to: '/admin/world/create',
    text: 'Create World'
  }
}

export const MANAGER_ROUTES = {
  links: [
    {
      to: '/admin/world',
      text: 'Worlds'
    },
    {
      to: '/admin/leagues',
      text: 'Leagues'
    },
    {
      to: '/admin/clubs',
      text: 'Clubs'
    },
    {
      to: '/admin/managers',
      text: 'managers'
    },
    {
      to: '/admin/players',
      text: 'Players'
    }
  ],
  cta: {
    to: '/admin/world/create',
    text: 'Create World'
  }
}
/** In-game sections, in design order. Counts are filled in by the layout from live data. */
export const GAME_NAV: ShellNavItem[] = [
  { label: 'Home', to: '/play', icon: PhHouse, primary: true },
  { label: 'Squad', to: '/play/squad', icon: PhUsersThree, primary: true },
  { label: 'Tactics', to: '/play/tactics', icon: PhStrategy, primary: true },
  { label: 'Training', to: '/play/training', icon: PhBarbell },
  { label: 'Transfers', to: '/play/transfers', icon: PhArrowsLeftRight, primary: true },
  { label: 'Scouting', to: '/play/scouting', icon: PhBinoculars },
  { label: 'Academy', to: '/play/academy', icon: PhStudent },
  { label: 'Finances', to: '/play/finances', icon: PhCoins },
  { label: 'Club', to: '/play/club', icon: PhShieldStar },
  { label: 'Competitions', to: '/play/competitions', icon: PhTrophy },
  { label: 'Fixtures', to: '/play/fixtures', icon: PhCalendarBlank },
  { label: 'World', to: '/play/world', icon: PhGlobeHemisphereWest },
  { label: 'Career', to: '/play/career', icon: PhBriefcase },
  { label: 'Social', to: '/play/social', icon: PhChatsCircle },
]
