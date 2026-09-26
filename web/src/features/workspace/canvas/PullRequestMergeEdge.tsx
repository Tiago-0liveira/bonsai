import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react'
import { GitPullRequest } from 'lucide-react'
import { PR_LABEL_SIZE, type PrLabelPlacement } from './layout/prLabels'
import { useBonsaiStore } from '../../../stores/bonsai'

export function PullRequestMergeEdge(props: EdgeProps) {
  const pullRequests = useBonsaiStore((state) => state.pullRequests)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const prNumber = Number(props.data?.prNumber ?? 0)
  const targetBranch = String(props.data?.targetBranch ?? '')
  const pr = pullRequests.find((item) => item.number === prNumber)

  const [path, labelX, labelY] = getBezierPath({
    sourceX: props.sourceX,
    sourceY: props.sourceY,
    sourcePosition: props.sourcePosition,
    targetX: props.targetX,
    targetY: props.targetY,
    targetPosition: props.targetPosition,
    curvature: 0.34,
  })

  const label = props.data?.labelPlacement as PrLabelPlacement | undefined

  return (
    <>
      <BaseEdge
        id={props.id}
        path={path}
        style={{
          stroke: 'rgb(151 109 255 / .72)',
          strokeWidth: 1.6,
          strokeDasharray: '6 5',
        }}
      />
      {label?.anchor && (
        <path
          d={`M${label.anchor.x},${label.anchor.y} L${label.x},${label.y}`}
          fill="none"
          stroke="rgb(151 109 255 / .5)"
          strokeDasharray="3 4"
          className="pointer-events-none"
        />
      )}
      <EdgeLabelRenderer>
        <button
          type="button"
          data-pr-edge-label={props.id}
          onClick={(event) => {
            event.stopPropagation()
            setNotice(pr ? 'Opened PR #' + pr.number + ' · ' + pr.title : 'PR relationship')
          }}
          className="nodrag nopan pointer-events-auto absolute flex items-center gap-1.5 rounded-full border border-[rgb(var(--purple)/.5)] bg-[rgb(var(--panel-2))] px-2 py-1 text-[8px] font-medium text-[rgb(var(--text))] shadow-lg hover:bg-[rgb(var(--panel-3))]"
          style={{ ...PR_LABEL_SIZE, transform: `translate(-50%, -50%) translate(${label?.x ?? labelX}px,${label?.y ?? labelY}px)` }}
          title={pr ? pr.title : 'Pull request merge relationship'}
        >
          <span className="grid h-5 w-5 shrink-0 place-items-center rounded-full bg-[rgb(var(--purple)/.15)] text-[rgb(var(--purple))]">
            <GitPullRequest size={11} />
          </span>
          <span className="shrink-0">{prNumber ? '#' + prNumber : 'PR'}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-[rgb(var(--muted))]">→ {targetBranch}</span>
        </button>
      </EdgeLabelRenderer>
    </>
  )
}
