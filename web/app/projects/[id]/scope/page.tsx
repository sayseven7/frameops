import PlanningWorkspace from "../../../planning-workspace";

type ProjectPageProps = { params: Promise<{ id: string }> };

export default async function ProjectScopePage({ params }: ProjectPageProps) {
  const { id } = await params;
  return <PlanningWorkspace engagementID={id} />;
}
