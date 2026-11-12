import type { Deps } from "../deps";
import { announcementEmail } from "../emails/announcement";
import * as admin from "./admin";

// sendAnnouncement records the announcement, then emails every subscribed user in the background.
export async function sendAnnouncement(deps: Deps, actorId: string, actorEmail: string, subject: string, body: string) {
  const to = await admin.recipients(deps.db);
  const record = await admin.recordAnnouncement(deps.db, actorId, actorEmail, subject, body, to.length);
  const messages = to.map((email) => announcementEmail(email, record!.subject, record!.body, deps.config.webUrl));
  deps.background(
    deps.mailer.sendMany(messages).then(async (failed) => {
      await admin.noteFailures(deps.db, record!.id, failed);
      console.log(`announcement ${record!.id} sent to ${to.length} recipients, ${failed} failed`);
    }),
  );
  return record;
}
