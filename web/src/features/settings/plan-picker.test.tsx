import { fireEvent, render, screen, within } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import type { CheckoutPlan } from '../../api/schemas';
import { PlanPicker } from './plan-picker';

const ladder: CheckoutPlan[] = ['solo_monthly', 'solo_yearly', 'small_monthly', 'small_yearly', 'team_monthly', 'team_yearly', 'team25_monthly', 'team25_yearly'];
const foundingOpen: CheckoutPlan[] = [...ladder, 'founding'];
const cardNames = () => screen.getAllByRole('article').map((card) => card.getAttribute('aria-label'));
const card = (name: string) => screen.getByRole('article', { name });
const primaries = () => document.querySelectorAll('.plan-picker .primary-action');

test('a team of three is offered Founding first, marked as fitting, with the reason above its one ink button', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} placesLeft={10} onChoose={vi.fn()} />);
  expect(cardNames()).toEqual(['Founding', 'Team 25']);
  const founding = card('Founding');
  expect(within(founding).getByText('Fits your team')).toBeInTheDocument();
  expect(within(founding).getByText('For teams of 3 to 10 agents')).toBeInTheDocument();
  expect(within(founding).getByText('$99')).toBeInTheDocument();
  expect(within(founding).getByText('every 3 months')).toBeInTheDocument();
  expect(within(founding).getByText('Price locked for life · 10 places left')).toBeInTheDocument();
  expect(within(founding).getByText('You have 3 agents. Founding covers up to 10.')).toBeInTheDocument();
  const team25 = card('Team 25');
  expect(within(team25).queryByText('Fits your team')).not.toBeInTheDocument();
  expect(within(team25).getByText('For teams of 11 to 25 agents')).toBeInTheDocument();
  expect(within(team25).getByText('$149')).toBeInTheDocument();
  expect(within(team25).getByText('a month')).toBeInTheDocument();
  expect(primaries()).toHaveLength(1);
  expect(primaries()[0]).toBe(within(founding).getByRole('button'));
  expect(within(team25).getByRole('button')).toBeEnabled();
});

test('the line about places left is left out when the count is not known', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} onChoose={vi.fn()} />);
  expect(within(card('Founding')).getByText('Price locked for life')).toBeInTheDocument();
});

test('a team of fourteen is offered Team 25 first, and Founding greyed with the reason it is too small', () => {
  render(<PlanPicker sellable={foundingOpen} seats={14} placesLeft={10} onChoose={vi.fn()} />);
  expect(cardNames()).toEqual(['Team 25', 'Founding']);
  expect(within(card('Team 25')).getByText('Fits your team')).toBeInTheDocument();
  expect(within(card('Team 25')).getByText('You have 14 agents.')).toBeInTheDocument();
  const founding = card('Founding');
  expect(founding).toHaveAttribute('data-fit', 'no');
  expect(within(founding).getByText('Up to 10 agents; you have 14.')).toBeInTheDocument();
  expect(within(founding).getByRole('button')).toBeDisabled();
  expect(primaries()).toHaveLength(1);
  expect(primaries()[0]).toBe(within(card('Team 25')).getByRole('button'));
});

test('the agent count of the Freshdesk account is named as its source and beats the workspace seats', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} freshdeskAgents={14} onChoose={vi.fn()} />);
  expect(cardNames()).toEqual(['Team 25', 'Founding']);
  expect(within(card('Team 25')).getByText('Your Freshdesk has 14 agents.')).toBeInTheDocument();
  expect(within(card('Founding')).getByText('Up to 10 agents; you have 14.')).toBeInTheDocument();
});

test('a small Freshdesk account says so beside what Founding covers', () => {
  render(<PlanPicker sellable={foundingOpen} seats={1} freshdeskAgents={1} onChoose={vi.fn()} />);
  expect(within(card('Founding')).getByText('Your Freshdesk has 1 agent. Founding covers up to 10.')).toBeInTheDocument();
});

test('a visitor who chose Team 25 on the website sees it first as their pick, and Founding still says that it also fits', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} preferredPlan="team25" onChoose={vi.fn()} />);
  expect(cardNames()).toEqual(['Team 25', 'Founding']);
  expect(within(card('Team 25')).getByText('Your pick')).toBeInTheDocument();
  expect(within(card('Team 25')).getByText('You chose Team 25 on colliedesk.com.')).toBeInTheDocument();
  expect(within(card('Founding')).getByText('You have 3 agents. Founding also fits.')).toBeInTheDocument();
  expect(within(card('Founding')).getByRole('button')).toBeEnabled();
  expect(primaries()).toHaveLength(1);
  expect(primaries()[0]).toBe(within(card('Team 25')).getByRole('button'));
});

test('a visitor who chose Team 25 for a team of fourteen sees Founding greyed as well', () => {
  render(<PlanPicker sellable={foundingOpen} seats={14} preferredPlan="team25" onChoose={vi.fn()} />);
  expect(within(card('Team 25')).getByText('Your pick')).toBeInTheDocument();
  expect(within(card('Founding')).getByText('Up to 10 agents; you have 14.')).toBeInTheDocument();
  expect(within(card('Founding')).getByRole('button')).toBeDisabled();
});

test('the billing period moves Team 25 only: Founding stays one price every 3 months', () => {
  const onChoose = vi.fn();
  render(<PlanPicker sellable={foundingOpen} seats={14} onChoose={onChoose} />);
  expect(screen.getByText('Team 25 billed')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('radio', { name: 'Yearly, 2 months free' }));
  const team25 = card('Team 25');
  expect(within(team25).getByText('$1,490')).toBeInTheDocument();
  expect(within(team25).getByText('a year')).toBeInTheDocument();
  expect(within(team25).getByText('2 months free')).toBeInTheDocument();
  expect(within(card('Founding')).getByText('$99')).toBeInTheDocument();
  expect(within(card('Founding')).getByText('every 3 months')).toBeInTheDocument();
  fireEvent.click(within(team25).getByRole('button', { name: 'Choose Team 25, $1,490 per year' }));
  expect(onChoose).toHaveBeenCalledWith('team25_yearly');
});

test('choosing Founding starts the Founding checkout', () => {
  const onChoose = vi.fn();
  render(<PlanPicker sellable={foundingOpen} seats={3} onChoose={onChoose} />);
  fireEvent.click(screen.getByRole('button', { name: 'Choose Founding, $99 per quarter for life' }));
  expect(onChoose).toHaveBeenCalledWith('founding');
});

test('while a checkout opens, its button says so and every other control waits', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} starting="founding" onChoose={vi.fn()} />);
  const founding = within(card('Founding')).getByRole('button');
  expect(founding).toHaveTextContent('Opening checkout…');
  expect(founding).toHaveAttribute('aria-busy', 'true');
  expect(founding).toBeDisabled();
  expect(within(card('Team 25')).getByRole('button')).toBeDisabled();
  for (const radio of screen.getAllByRole('radio')) expect(radio).toBeDisabled();
});

test('a checkout that did not open says nothing was charged and offers Try again', () => {
  const retry = vi.fn();
  render(<PlanPicker sellable={foundingOpen} seats={3} error="Checkout did not open. Nothing was charged." onRetry={retry} onChoose={vi.fn()} />);
  const alert = screen.getByRole('alert');
  expect(alert).toHaveTextContent('Checkout did not open. Nothing was charged.');
  fireEvent.click(within(alert).getByRole('button', { name: 'Try again' }));
  expect(retry).toHaveBeenCalledTimes(1);
});

test('a reason the server gave stays in words with no Try again, since trying again would not help', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} error="Polar did not accept your address." onChoose={vi.fn()} />);
  expect(screen.getByRole('alert')).toHaveTextContent('Polar did not accept your address.');
  expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
});

test('a plan the server says this workspace does not fit shows the server reason in the fit line and cannot be chosen', () => {
  render(<PlanPicker sellable={foundingOpen} seats={14} refusals={[{ plan: 'team25_monthly', reason: 'Team 25 holds 250 GiB; you store 300 GiB.' }, { plan: 'team25_yearly', reason: 'Team 25 holds 250 GiB; you store 300 GiB.' }]} onChoose={vi.fn()} />);
  const team25 = card('Team 25');
  expect(within(team25).getByText('Team 25 holds 250 GiB; you store 300 GiB.')).toBeInTheDocument();
  expect(within(team25).getByRole('button')).toBeDisabled();
});

test('a team larger than Team 25 is told where to write, and nothing about a free self-hosted copy is pushed', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} onChoose={vi.fn()} />);
  expect(screen.getByText(/larger than 25 agents\?/i)).toHaveTextContent('Larger than 25 agents? Email support@colliedesk.com');
  expect(screen.getByRole('link', { name: 'support@colliedesk.com' })).toHaveAttribute('href', 'mailto:support@colliedesk.com');
  expect(screen.getByText('Pay by card through Polar. Cancel any time.')).toBeInTheDocument();
  expect(screen.queryByText(/self-host/i)).not.toBeInTheDocument();
});

test('the compact picker of the locked screen leaves out what each plan includes and the notes', () => {
  render(<PlanPicker sellable={foundingOpen} seats={3} compact onChoose={vi.fn()} />);
  expect(screen.queryByText('20 GiB attachment storage')).not.toBeInTheDocument();
  expect(screen.queryByText(/pay by card through polar/i)).not.toBeInTheDocument();
  expect(primaries()).toHaveLength(1);
});

test('once Founding is closed the whole ladder is offered, smallest first, and the smallest plan that holds the team leads', () => {
  render(<PlanPicker sellable={ladder} seats={2} onChoose={vi.fn()} />);
  expect(cardNames()).toEqual(['Solo', 'Small', 'Team', 'Team 25']);
  expect(within(card('Small')).getByText('Fits your team')).toBeInTheDocument();
  expect(within(card('Solo')).queryByText('Fits your team')).not.toBeInTheDocument();
  expect(primaries()).toHaveLength(1);
  expect(primaries()[0]).toBe(within(card('Small')).getByRole('button'));
  expect(screen.getByRole('group', { name: 'Billing interval' })).toBeInTheDocument();
});
