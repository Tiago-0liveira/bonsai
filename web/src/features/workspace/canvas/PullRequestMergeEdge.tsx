import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react'
import { GitPullRequest } from 'lucide-react'
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
      <EdgeLabelRenderer>
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            setNotice(pr ? 'Opened PR #' + pr.number + ' · ' + pr.title : 'PR relationship')
          }}
          className="nodrag nopan pointer-events-auto absolute flex items-center gap-1.5 rounded-full border border-[rgb(var(--purple)/.5)] bg-[rgb(var(--panel-2))] px-2 py-1 text-[8px] font-medium text-[rgb(var(--text))] shadow-lg hover:bg-[rgb(var(--panel-3))]"
          style={{ transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)` }}
          title={pr ? pr.title : 'Pull request merge relationship'}
        >
          <span className="grid h-5 w-5 place-items-center rounded-full bg-[rgb(var(--purple)/.15)] text-[rgb(var(--purple))]">
            <GitPullRequest size={11} />
          </span>
          <span>{prNumber ? '#' + prNumber : 'PR'}</span>
          <span className="max-w-[120px] truncate font-mono text-[rgb(var(--muted))]">→ {targetBranch}</span>
        </button>
      </EdgeLabelRenderer>
    </>
  )
}
