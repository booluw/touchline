export * from "./user"
export * from "./api"
export * from "./auth"
export * from "./admin"
export * from "./manager"
// admin and manager both declare these; pick the side each consumer of "~/types" uses.
export type { League } from "./admin"
export type { Cup, Fixture, NextFixture } from "./manager"
