export function filterKeys(values: Record<string, string>, query: string) {
  const needle = query.toLowerCase();
  return Object.keys(values).filter((key) => key.toLowerCase().includes(needle)).sort();
}

export function initials(email = "") {
  return email.slice(0, 2).toUpperCase() || "EN";
}
