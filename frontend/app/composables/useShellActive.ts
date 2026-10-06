import type { ShellNavItem } from '~/types/ui/design'

/** Active-route matcher for shell nav: section roots match exactly, others by prefix. */
export function useShellActive() {
  const route = useRoute()
  return (item: ShellNavItem) =>
    item.to === '/play' ? route.path === '/play' || route.path === '/play/' : route.path === item.to || route.path.startsWith(`${item.to}/`)
}
