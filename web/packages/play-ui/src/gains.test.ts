import { test } from "node:test";
import assert from "node:assert/strict";
import { gainedItems, mergeGains } from "./gains.ts";

const needle = { slug: "needle", name: "Needle", qty: 1 };
const coin = { slug: "coin", name: "Coin", qty: 2 };

test("a new item is a gain", () => {
  assert.deepEqual(gainedItems([], [needle]), [needle]);
  assert.deepEqual(gainedItems(undefined, [needle]), [needle]);
});

test("more of an item gains the difference", () => {
  assert.deepEqual(gainedItems([coin], [{ ...coin, qty: 5 }]), [{ ...coin, qty: 3 }]);
});

test("nothing new, an item used up or handed away is no gain", () => {
  assert.deepEqual(gainedItems([needle, coin], [needle, coin]), []);
  assert.deepEqual(gainedItems([needle, coin], [{ ...coin, qty: 1 }]), []);
  assert.deepEqual(gainedItems([needle], undefined), []);
});

test("an item held in two entries counts once across them", () => {
  assert.deepEqual(gainedItems([{ ...coin, qty: 2 }], [{ ...coin, qty: 1, holder: "Ann" }, { ...coin, qty: 1 }]), []);
  assert.deepEqual(gainedItems([{ ...coin, qty: 2 }], [{ ...coin, qty: 2, holder: "Ann" }, { ...coin, qty: 1 }]), [{ ...coin, qty: 1 }]);
});

test("gains shown together add up", () => {
  assert.deepEqual(mergeGains([needle], [{ ...needle, qty: 2 }, coin]), [{ ...needle, qty: 3 }, coin]);
});
