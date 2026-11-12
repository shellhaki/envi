// The install scripts are imported as text (wrangler [[rules]] when deployed, test/text-imports.ts in tests).
declare module "*.sh" {
  const text: string;
  export default text;
}
declare module "*.ps1" {
  const text: string;
  export default text;
}
