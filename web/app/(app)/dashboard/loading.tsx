// Deliberately empty. Its presence is what matters: it lets Next prefetch the
// dashboard routes and commit a click immediately instead of waiting on the
// server. Workspace lives in the layout and draws its own skeletons.
export default function Loading() {
  return null;
}
