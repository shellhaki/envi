// The install scripts are imported as text (wrangler [[rules]] for Workers, a test preload for Bun).
declare module "*.sh" {
  const text: string;
  export default text;
}
declare module "*.ps1" {
  const text: string;
  export default text;
}
