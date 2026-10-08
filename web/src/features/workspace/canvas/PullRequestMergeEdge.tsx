import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from '@xyflow/react'
import { PR_LABEL_SIZE, type PrLabelPlacement } from './layout/prLabels'
import { useBonsaiStore } from '../../../stores/bonsai'

export function PullRequestMergeEdge(props: EdgeProps) {
  const pullRequests = useBonsaiStore((state) => state.pullRequests)
  const setNotice = useBonsaiStore((state) => state.setNotice)
  const prNumber = Number(props.data?.prNumber ?? 0)
  const projectId = String(props.data?.projectId ?? '')
  const targetBranch = String(props.data?.targetBranch ?? '')
  const pr = pullRequests.find((item) => item.id === `${projectId}:${prNumber}`)

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
        markerEnd={props.markerEnd}
        style={{ stroke: 'rgb(var(--ok) / .8)', strokeWidth: 2 }}
      />
      {label?.anchor && (
        <path
          d={`M${label.anchor.x},${label.anchor.y} L${label.x},${label.y}`}
          fill="none"
          style={{ stroke: 'rgb(var(--ok) / .5)' }}
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
          className="nodrag nopan pointer-events-auto absolute grid place-items-center"
          style={{ ...PR_LABEL_SIZE, transform: `translate(-50%, -50%) translate(${label?.x ?? labelX}px,${label?.y ?? labelY}px)` }}
          title={pr ? pr.title : 'Pull request merge relationship'}
        >
          <span className="h-[18px] max-w-full truncate rounded-full border border-ok/40 bg-bg px-2 font-mono text-[10px] leading-[16px] text-ok">
            PR {prNumber ? '#' + prNumber : ''} → {targetBranch}
          </span>
        </button>
      </EdgeLabelRenderer>
    </>
  )
}
