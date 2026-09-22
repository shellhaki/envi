/**
 * Three ways this can fail, kept apart because the fix for each is different:
 * check your credential, check your names, or check the network.
 */

export class EnviError extends Error {
  constructor(message: string) {
    super(message);
    this.name = new.target.name;
  }
}

/** The credential is missing, wrong, expired, or revoked. */
export class EnviAuthError extends EnviError {}

/** No such project or environment. The message lists what does exist. */
export class EnviNotFoundError extends EnviError {}

/** Envi could not be reached, or did not answer in time. */
export class EnviUnreachableError extends EnviError {
  constructor(message: string, readonly cause?: unknown) {
    super(message);
  }
}

/** A key was required and is not there. */
export class EnviMissingKeyError extends EnviError {}
