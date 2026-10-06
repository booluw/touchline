export * from "./user"
export * from "./api"
export * from "./_auth"
export * from "./_admin"
export * from "./_manager"
// admin and manager both declare these; pick the side each consumer of "~/types" uses.
export type { League } from "./_admin"
export type { Cup, Fixture, NextFixture } from "./_manager"

export * from "./_player"
export type LoadingStatus = "loading" | "loaded" | "error"