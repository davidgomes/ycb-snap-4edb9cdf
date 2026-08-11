import { describe, expect, it } from "bun:test";
import { setImmediate, setInterval, setTimeout } from "node:timers/promises";

describe("setTimeout", () => {
  it("abort() does not emit global error", async () => {
    let unhandledRejectionCaught = false;

    const catchUnhandledRejection = () => {
      unhandledRejectionCaught = true;
    };
    process.on("unhandledRejection", catchUnhandledRejection);

    const c = new AbortController();

    global.setTimeout(() => c.abort());

    await setTimeout(100, undefined, { signal: c.signal }).catch(() => "aborted");

    // let unhandledRejection to be fired
    await setTimeout(100);

    process.off("unhandledRejection", catchUnhandledRejection);

    expect(c.signal.aborted).toBe(true);
    expect(unhandledRejectionCaught).toBe(false);
  });

  it("AbortController can be passed as the `options` argument", () => {
    expect(async () => await setTimeout(0, undefined, new AbortController())).not.toThrow();
  });

  it("should reject promise when AbortController is aborted", async () => {
    const abortController = new AbortController();
    const promise = setTimeout(100, undefined, abortController);
    abortController.abort();

    await expect(promise).rejects.toThrow(expect.objectContaining({ name: "AbortError" }));
    expect(abortController.signal.aborted).toBe(true);
  });

  it("rejects when abort listener calls stopImmediatePropagation", async () => {
    const abortController = new AbortController();
    abortController.signal.addEventListener("abort", e => e.stopImmediatePropagation(), { once: true });
    const promise = setTimeout(60_000, undefined, { signal: abortController.signal });
    abortController.abort();
    await Promise.resolve();
    expect(Bun.peek.status(promise)).toBe("rejected");
    await expect(promise).rejects.toMatchObject({ name: "AbortError" });
  });
});

describe("setImmediate", () => {
  it("abort() does not emit global error", async () => {
    let unhandledRejectionCaught = false;

    const catchUnhandledRejection = () => {
      unhandledRejectionCaught = true;
    };
    process.on("unhandledRejection", catchUnhandledRejection);

    const c = new AbortController();

    global.setImmediate(() => c.abort());

    await setImmediate(undefined, { signal: c.signal }).catch(() => "aborted");

    // let unhandledRejection to be fired
    await setTimeout(100);

    process.off("unhandledRejection", catchUnhandledRejection);

    expect(c.signal.aborted).toBe(true);
    expect(unhandledRejectionCaught).toBe(false);
  });

  it("rejects when abort listener calls stopImmediatePropagation", async () => {
    const abortController = new AbortController();
    abortController.signal.addEventListener("abort", e => e.stopImmediatePropagation(), { once: true });
    const promise = setImmediate(undefined, { signal: abortController.signal });
    abortController.abort();
    await Promise.resolve();
    expect(Bun.peek.status(promise)).toBe("rejected");
    await expect(promise).rejects.toMatchObject({ name: "AbortError" });
  });
});

describe("setInterval", () => {
  it("ends with AbortError when abort listener calls stopImmediatePropagation", async () => {
    const abortController = new AbortController();
    abortController.signal.addEventListener("abort", e => e.stopImmediatePropagation(), { once: true });
    const iterator = setInterval(60_000, "tick", { signal: abortController.signal });
    const nextPromise = iterator.next();
    abortController.abort();
    await Promise.resolve();
    expect(Bun.peek.status(nextPromise)).toBe("rejected");
    await expect(nextPromise).rejects.toMatchObject({ name: "AbortError" });
  });
});
