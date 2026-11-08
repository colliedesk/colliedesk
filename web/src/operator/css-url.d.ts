// A stylesheet imported as its URL (Vite's ?url), for a lazy part of the console that links its own styles.
declare module '*.css?url' {
  const href: string;
  export default href;
}
