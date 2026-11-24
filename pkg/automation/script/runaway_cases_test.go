// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package script

// runawayCase is a script that does its work inside one call of native code of the interpreter (a built-in that
// loops over a length or a size the script chooses, the parser, the regexp engine, big integers), where the
// interrupt of the timeout is never seen.
type runawayCase struct {
	name string
	src  string
}

// runawayCases is the survey of such calls. Each one either loops for much longer than any script timeout or
// allocates until memory runs out, all inside a single native call.
var runawayCases = []runawayCase{
	// Array.prototype methods applied to an array-like with a huge length.
	{"indexOf on array-like", `Array.prototype.indexOf.call({length: 2**53 - 1}, 1)`},
	{"lastIndexOf on array-like", `Array.prototype.lastIndexOf.call({length: 2**53 - 1}, 1)`},
	{"includes on array-like", `Array.prototype.includes.call({length: 2**53 - 1}, 1)`},
	{"fill on array-like", `Array.prototype.fill.call({length: 2**53 - 1}, 1)`},
	{"join on array-like", `Array.prototype.join.call({length: 2**53 - 1})`},
	{"join of new Array(2**32-1)", `new Array(2**32 - 1).join()`},
	{"reverse on array-like", `Array.prototype.reverse.call({length: 2**53 - 1})`},
	{"sort on array-like", `Array.prototype.sort.call({length: 2**53 - 1})`},
	{"sort of sparse array", `var a = []; a.length = 2**32 - 1; a.sort()`},
	{"sort of sparse array, script comparator", `var a = []; a.length = 2**32 - 1; a.sort(function(x, y){ return 0 })`},
	{"copyWithin on array-like", `Array.prototype.copyWithin.call({length: 2**53 - 1}, 0, 1)`},
	{"find with Boolean", `Array.prototype.find.call({length: 2**53 - 1}, Boolean)`},
	{"findIndex with Boolean", `Array.prototype.findIndex.call({length: 2**53 - 1}, Boolean)`},
	{"findLast with Boolean", `Array.prototype.findLast.call({length: 2**53 - 1}, Boolean)`},
	{"findLastIndex with Boolean", `Array.prototype.findLastIndex.call({length: 2**53 - 1}, Boolean)`},
	{"every with Object", `Array.prototype.every.call({length: 2**53 - 1}, Object)`},
	{"some with Boolean", `Array.prototype.some.call({length: 2**53 - 1}, Boolean)`},
	{"forEach with Boolean", `Array.prototype.forEach.call({length: 2**53 - 1}, Boolean)`},
	{"map with Boolean", `Array.prototype.map.call({length: 2**32 - 1}, Boolean)`},
	{"filter with Boolean", `Array.prototype.filter.call({length: 2**53 - 1}, Boolean)`},
	{"reduce with Math.max", `Array.prototype.reduce.call({length: 2**53 - 1}, Math.max, 0)`},
	{"reduceRight with Math.max", `Array.prototype.reduceRight.call({length: 2**53 - 1}, Math.max, 0)`},
	{"flat on array-like", `Array.prototype.flat.call({length: 2**53 - 1})`},
	{"flatMap with Boolean", `Array.prototype.flatMap.call({length: 2**53 - 1}, Boolean)`},
	{"concat of spreadable array-like", `[].concat({length: 2**53 - 1, [Symbol.isConcatSpreadable]: true})`},
	{"slice on array-like", `Array.prototype.slice.call({length: 2**32 - 1}, 0)`},
	{"splice on array-like", `Array.prototype.splice.call({length: 2**53 - 2}, 0, 0, 1)`},
	{"unshift on array-like", `Array.prototype.unshift.call({length: 2**53 - 2}, 1)`},
	{"shift on array-like", `Array.prototype.shift.call({length: 2**53 - 1})`},
	{"toReversed on array-like", `Array.prototype.toReversed.call({length: 2**32 - 1})`},
	{"toSorted on array-like", `Array.prototype.toSorted.call({length: 2**32 - 1})`},
	{"with on array-like", `Array.prototype.with.call({length: 2**32 - 1}, 0, 1)`},
	{"indexOf through a Proxy with no traps", `Array.prototype.indexOf.call(new Proxy({length: 2**53 - 1}, {}), 1)`},
	{"indexOf through Reflect.apply", `Reflect.apply(Array.prototype.indexOf, {length: 2**53 - 1}, [1])`},
	{"indexOf through a bound function", `Array.prototype.indexOf.bind({length: 2**53 - 1})(1)`},
	{"indexOf through [].constructor", `[].constructor.prototype.indexOf.call({length: 2**53 - 1}, 1)`},
	// Creating and spreading huge arrays.
	{"Array.from array-like", `Array.from({length: 2**32 - 1})`},
	{"spread of a native iterator", `[...Array.prototype.values.call({length: 2**32 - 1})]`},
	{"Object.fromEntries of a native iterator", `Object.fromEntries(Array.prototype.values.call({length: 2**53 - 1}))`},
	{"new Set of keys of a sparse array", `new Set(new Array(2**32 - 1).keys())`},
	{"Function.prototype.apply with array-like", `Math.max.apply(null, {length: 2**32 - 1})`},
	{"Reflect.apply with array-like", `Reflect.apply(Math.max, null, {length: 2**32 - 1})`},
	{"String.raw with array-like", `String.raw({raw: {length: 2**53 - 1}})`},
	{"Object.assign of a long string", `Object.assign({}, "x".repeat(2**27))`},
	{"Object.keys of a long string", `Object.keys("x".repeat(2**28))`},
	{"Reflect.ownKeys of a typed array", `Reflect.ownKeys(new Uint8Array(2**28))`},
	// Typed arrays.
	{"new Uint8Array(2**32)", `new Uint8Array(2**32)`},
	{"new Float64Array(2**28).sort()", `new Float64Array(2**28).sort()`},
	{"new Uint8Array(array-like)", `new Uint8Array({length: 2**32})`},
	{"Uint8Array.from array-like", `Uint8Array.from({length: 2**32})`},
	{"typed array indexOf", `new Uint8Array(2**31).indexOf(1)`},
	{"new ArrayBuffer(2**32)", `new ArrayBuffer(2**32)`},
	// Strings.
	{"repeat", `"x".repeat(2**30).length`},
	{"padStart", `"".padStart(2**30, "ab").length`},
	{"padEnd", `"".padEnd(2**30, "ab").length`},
	{"repeat then split", `"x".repeat(2**26).split("").length`},
	{"string doubling then indexOf", `var s = "xy"; for (var i = 0; i < 31; i++) { s += s } return s.indexOf("z")`},
	{"replace with many matches", `"a".repeat(2**26).replace(/a/g, "bb").length`},
	{"catastrophic regexp", `/^(a+)+$/.test("a".repeat(40) + "b")`},
	{"catastrophic regexp with backreference", `/^(a+)+\1$/.test("a".repeat(40) + "b")`},
	// JSON, the parser and the host boundary.
	{"JSON.stringify of a huge sparse array", `JSON.stringify(new Array(2**30)).length`},
	{"JSON.parse of deep nesting", `JSON.parse("[".repeat(2**24) + "]".repeat(2**24))`},
	{"eval of a long expression", `eval("1+".repeat(2**22) + "1")`},
	{"eval of deep nesting", `eval("[".repeat(2**22) + "]".repeat(2**22))`},
	{"new Function with a huge body", `new Function("return " + "1+".repeat(2**22) + "1")`},
	{"log of a huge sparse array", `log(new Array(2**30))`},
	{"return of a huge sparse array", `return new Array(2**30)`},
	// Numbers.
	{"BigInt exponentiation", `(7n ** (2n ** 32n)).toString().length`},
	{"BigInt toString", `(3n ** 50000000n).toString().length`},
	{"toString of a huge number with radix", `(2**1023).toString(3).length`},
}
