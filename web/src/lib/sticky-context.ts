/** Table-scoped reactive getter; descendants never read persistence themselves. */
export const STICKY_SOURCES = Symbol('sticky-sources');
export type StickySources = () => readonly string[];
