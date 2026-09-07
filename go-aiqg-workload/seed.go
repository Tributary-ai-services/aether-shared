package workload

// The Tier-A seed vocabulary: a shipped class space carrying no customer data
// at all, so a tenant has a working partition of their traffic on day one
// rather than in week six.
//
// # Every class here is a hypothesis, and says so
//
// Seed classes carry Origin "seed" and no SeparationEvidence, which is the
// same standing a global prior has: they may seed, propose and prioritise, and
// they may NOT gate a routing decision until they have separated on the
// tenant's own traffic. Nothing downstream should treat a seed assignment as a
// measurement.
//
// # What building this actually showed: four, not seven
//
// The design sketched seven coding intents from the workflow taxonomy —
// discovery, modification, generation, diagnosis, validation, execution,
// orchestration. Only FOUR of them are separable from the structural feature
// vector, and pretending otherwise would have produced exactly the confident
// misclassification the whole design is shaped to avoid:
//
//   - GENERATION vs MODIFICATION needs to know whether the edit target already
//     existed. That is a property of the filesystem, not of the request, and
//     it is not in the vector. (The design already flagged this as an open
//     question — this is the answer: they are one class until something else
//     separates them.)
//   - DIAGNOSIS is reasoning with no tool calls, which is structurally
//     identical to conversation. Distinguishing them needs content.
//   - VALIDATION vs EXECUTION both run commands. "go test" and "git push" are
//     the same shape; only the command string separates them, and the command
//     string is content the extractor deliberately does not read.
//
// So the seed offers four coding classes and names the gap. The missing three
// are precisely the work that labelling (customers naming their own segments)
// and discovery (the separation test) exist to do — which is a better argument
// for those phases than the design made on its own.

// SeedVersion identifies this artifact. Consumers store it beside every
// assignment, so a class space change is a version bump rather than numbers
// that quietly moved.
const SeedVersion = "seed-1"

// SeedSpace returns the shipped starting partition.
func SeedSpace() Space {
	seed := func(id, label, desc string, rules ...Rule) Class {
		return Class{
			ID: id, Label: label, Description: desc,
			Origin: OriginSeed, Status: StatusProposed, Rules: rules,
		}
	}
	num := func(feat string, op Op, n float64) Cond { return Cond{Feature: feat, Op: op, Num: n} }

	return Space{
		Version: SeedVersion,
		Classes: []Class{
			// ---- Coding: separable from tool-family mix ----
			// Thresholds are 0.6 of an EXPLICIT family share, not 0.34 of
			// whichever family won a tie-break. Two things went wrong with the
			// earlier form and both are worth remembering: a bare plurality
			// let precedence decide a two-call turn, and — caught by a test —
			// discovery is a TWO-family concept, so a turn split evenly
			// between reading and grepping is entirely discovery while being
			// dominant in neither. Hence readonly_share.
			seed("code.modification", "Code modification",
				"Editing, patching or refactoring existing code.",
				Rule{
					AllOf:      []Cond{num(FShareEdit, OpGte, 0.6)},
					Confidence: 0.8,
					Note:       "most tool calls in this turn were edits",
				}),
			seed("code.discovery", "Code discovery",
				"Reading, grepping and exploring a repository.",
				Rule{
					AllOf:      []Cond{num(FReadonlyShare, OpGte, 0.6)},
					Confidence: 0.8,
					Note:       "most tool calls in this turn read or searched",
				}),
			seed("code.execution", "Command execution",
				"Running commands: tests, builds, git, deploys. Validation and execution are not yet separable — see the package note.",
				Rule{
					AllOf:      []Cond{num(FShareExec, OpGte, 0.6)},
					Confidence: 0.75,
					Note:       "most tool calls in this turn ran commands",
				}),
			seed("code.orchestration", "Orchestration",
				"Planning, decomposition and delegation to sub-agents.",
				Rule{
					AllOf:      []Cond{num(FShareDelegate, OpGte, 0.6)},
					Confidence: 0.75,
					Note:       "most tool calls in this turn delegated or tracked work",
				}),

			// ---- Non-coding archetypes ----
			seed("extraction.structured", "Structured extraction",
				"A schema-constrained response — the one archetype the request declares outright.",
				Rule{
					AllOf:      []Cond{num(FJSONSchemaOut, OpEq, 1)},
					Confidence: 0.9,
					Note:       "the request asked for a JSON schema-constrained response",
				}),
			seed("rag.qa", "Retrieval-augmented Q&A",
				"Context fenced into the prompt and answered from.",
				Rule{
					AllOf:      []Cond{num(FHasTools, OpEq, 0), num(FRetrievalMarkers, OpGte, 3)},
					Confidence: 0.75,
					Note:       "three or more retrieval markers fenced context into the prompt",
				}),
			// Depth is load-bearing here, and it was learned the hard way.
			// Without it this rule claimed 52.6% of 30,082 real Claude Code
			// turns: a deep agentic turn that calls no tool has an enormous
			// cached input and a short answer, which is ratio-identical to a
			// summarization and is not one. Requiring a shallow conversation
			// pushes those turns to unclassified, which is the true answer —
			// see UnseparableIntents.
			seed("summarization", "Summarization",
				"Long input, short output, near the start of a shallow conversation.",
				Rule{
					AllOf: []Cond{
						num(FHasTools, OpEq, 0),
						num(FInOutRatio, OpGte, 15),
						num(FInputTokens, OpGte, 1000),
						num(FDepth, OpLte, 2),
					},
					Confidence: 0.6,
					Note:       "input dwarfs output by 15x or more, early in a shallow exchange",
				}),
			seed("conversation", "Conversation",
				"Multi-turn back-and-forth with no tools and no fenced context.",
				Rule{
					AllOf: []Cond{
						num(FHasTools, OpEq, 0),
						num(FDepth, OpGte, 1),
						num(FRetrievalMarkers, OpEq, 0),
						num(FInputTokens, OpLt, 4000),
					},
					Confidence: 0.5,
					Note:       "a follow-up turn with no tools and no fenced context",
				}),
			seed("single_turn_qa", "Single-turn Q&A",
				"One short question, answered once.",
				Rule{
					AllOf: []Cond{
						num(FHasTools, OpEq, 0),
						num(FDepth, OpEq, 0),
						num(FMessageCount, OpLte, 2),
						num(FInputTokens, OpLt, 1500),
						num(FRetrievalMarkers, OpEq, 0),
					},
					Confidence: 0.5,
					Note:       "a single short turn with no tools and no fenced context",
				}),
		},
	}
}

// UnseparableIntents names the coding intents the structural vector cannot
// distinguish, and what each would need.
//
// Exported rather than left as a comment because a surface showing the class
// space should be able to say what is deliberately missing. "We offer four
// coding classes" invites the question; this answers it in the product rather
// than in a design document nobody reading the screen has open.
func UnseparableIntents() map[string]string {
	return map[string]string{
		"code.generation": "needs to know whether the edit target already existed — a property of the filesystem, not the request",
		"code.diagnosis":  "reasoning with no tool calls is structurally identical to conversation; separating them needs content",
		"code.validation": "distinguishing a test run from any other command needs the command string, which the extractor does not read",
	}
}

// ExecutionConflates names what code.execution is known to fold together.
//
// Measured against 30,094 real Claude Code turns, code.execution took 31.3% of
// traffic against code.discovery's 3.2% — not a threshold artifact, but the
// direct consequence of agents doing much of their reading THROUGH a shell.
// A grep run via a Bash tool is discovery performed by an exec-family tool,
// and family classification attributes intent to the TOOL, not to the action.
//
// This is the sharpest known limit of structural classification, and it is
// also the clearest case for labelling: an operator who names "our test runs"
// as a segment supplies exactly the discriminator the vector cannot see.
func ExecutionConflates() []string {
	return []string{
		"running tests and builds",
		"git and deployment operations",
		"discovery performed through a shell (grep, find, cat) rather than a dedicated read tool",
	}
}
