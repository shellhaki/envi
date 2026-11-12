export class Forbidden extends Error {
  constructor(message = "forbidden") {
    super(message);
  }
}

export class NotFound extends Error {
  constructor(message = "not found") {
    super(message);
  }
}

export class Conflict extends Error {
  constructor(message = "conflict") {
    super(message);
  }
}

export class RateLimited extends Error {}

// Invalid is a problem with the caller's input; its message is safe to show them.
export class Invalid extends Error {}
